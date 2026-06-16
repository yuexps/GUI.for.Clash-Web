package bridge

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"guiforcores/bridge/runtime"
	"gopkg.in/yaml.v3"
)

type ScheduledTask struct {
	ID            string   `yaml:"id"`
	Name          string   `yaml:"name"`
	Type          string   `yaml:"type"`
	Cron          string   `yaml:"cron"`
	Disabled      bool     `yaml:"disabled"`
	Subscriptions []string `yaml:"subscriptions"`
	Rulesets      []string `yaml:"rulesets"`
	Plugins       []string `yaml:"plugins"`
	Script        string   `yaml:"script"`
	Notification  bool     `yaml:"notification"`
	LastTime      int64    `yaml:"lastTime"`
}

type Subscription struct {
	ID               string      `yaml:"id"`
	Name             string      `yaml:"name"`
	UseInternal      bool        `yaml:"useInternal"`
	Upload           int64       `yaml:"upload"`
	Download         int64       `yaml:"download"`
	Total            int64       `yaml:"total"`
	Expire           int64       `yaml:"expire"`
	UpdateTime       int64       `yaml:"updateTime"`
	Type             string      `yaml:"type"` // Http, File, Manual
	URL              string      `yaml:"url"`
	Path             string      `yaml:"path"`
	Include          string      `yaml:"include"`
	Exclude          string      `yaml:"exclude"`
	IncludeProtocol  string      `yaml:"includeProtocol"`
	ExcludeProtocol  string      `yaml:"excludeProtocol"`
	ProxyPrefix      string      `yaml:"proxyPrefix"`
	RequestProxyMode string      `yaml:"requestProxyMode"`
	CustomProxy      string      `yaml:"customProxy"`
	Disabled         bool        `yaml:"disabled"`
	InSecure         bool        `yaml:"inSecure"`
	RequestMethod    string      `yaml:"requestMethod"`
	RequestTimeout   int         `yaml:"requestTimeout"`
	Header           SubHeaders  `yaml:"header"`
	Proxies          []ProxyItem `yaml:"proxies"`
}

type SubHeaders struct {
	Request  map[string]string `yaml:"request"`
	Response map[string]string `yaml:"response"`
}

type ProxyItem struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
	Type string `yaml:"type"`
}

type Ruleset struct {
	ID         string `yaml:"id"`
	Name       string `yaml:"name"`
	UpdateTime int64  `yaml:"updateTime"`
	Type       string `yaml:"type"`
	Behavior   string `yaml:"behavior"`
	Format     string `yaml:"format"`
	URL        string `yaml:"url"`
	Path       string `yaml:"path"`
	Count      int    `yaml:"count"`
	Disabled   bool   `yaml:"disabled"`
}

type TaskManager struct {
	app   *App
	cron  *cron.Cron
	mu    sync.Mutex
	tasks []ScheduledTask
}

var taskMgr *TaskManager

func InitTaskManager(app *App) {
	taskMgr = &TaskManager{
		app:  app,
		cron: cron.New(cron.WithSeconds()),
	}
	err := taskMgr.ReloadTasks()
	if err != nil {
		log.Printf("[TaskManager] Init reload tasks error: %v", err)
	}
	taskMgr.cron.Start()
	log.Printf("[TaskManager] Backend Cron Scheduler started")
}

func GetTaskManager() *TaskManager {
	return taskMgr
}

func (tm *TaskManager) ReloadTasks() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.cron.Stop()
	tm.cron = cron.New(cron.WithSeconds())

	tasksPath := resolvePath("data/scheduledtasks.yaml")
	data, err := os.ReadFile(tasksPath)
	if err != nil {
		if os.IsNotExist(err) {
			tm.tasks = nil
			return nil
		}
		return err
	}

	var tasks []ScheduledTask
	if err := yaml.Unmarshal(data, &tasks); err != nil {
		return err
	}
	tm.tasks = tasks

	for _, task := range tasks {
		if task.Disabled || task.Cron == "" {
			continue
		}

		t := task
		_, err := tm.cron.AddFunc(t.Cron, func() {
			tm.RunTask(t.ID)
		})
		if err != nil {
			log.Printf("[TaskManager] Failed to add job for task %s (%s): %v", t.Name, t.Cron, err)
		} else {
			log.Printf("[TaskManager] Registered cron task: %s (%s)", t.Name, t.Cron)
		}
	}

	tm.cron.Start()
	return nil
}

func (tm *TaskManager) RunTask(id string) {
	tm.mu.Lock()
	var task *ScheduledTask
	var taskIndex = -1
	for i := range tm.tasks {
		if tm.tasks[i].ID == id {
			task = &tm.tasks[i]
			taskIndex = i
			break
		}
	}
	tm.mu.Unlock()

	if task == nil {
		log.Printf("[TaskManager] Task ID not found: %s", id)
		return
	}

	if task.Disabled {
		log.Printf("[TaskManager] Task [%s] is disabled", task.Name)
		return
	}

	log.Printf("[TaskManager] Executing task: %s (%s)", task.Name, task.Type)
	startTime := time.Now().UnixMilli()

	var results []any

	switch task.Type {
	case "update::subscription":
		results = tm.updateSubscriptions(task.Subscriptions)
	case "update::all::subscription":
		results = tm.updateAllSubscriptions()
	case "update::ruleset":
		results = tm.updateRulesets(task.Rulesets)
	case "update::all::ruleset":
		results = tm.updateAllRulesets()
	case "update::plugin", "update::all::plugin":
		results = append(results, map[string]any{
			"ok":     false,
			"result": "Plugin updates are not supported natively in backend yet.",
		})
	case "run::plugin", "run::script":
		results = append(results, map[string]any{
			"ok":     false,
			"result": "Running custom JS scripts is not supported in headless mode. Run via WebUI.",
		})
	default:
		results = append(results, map[string]any{
			"ok":     false,
			"result": fmt.Sprintf("Unsupported task type: %s", task.Type),
		})
	}

	endTime := time.Now().UnixMilli()

	if taskIndex != -1 {
		tm.mu.Lock()
		tm.tasks[taskIndex].LastTime = endTime
		tm.mu.Unlock()
		tm.saveTasks()
	}

	logRecord := map[string]any{
		"name":      task.Name,
		"startTime": startTime,
		"endTime":   endTime,
		"result":    results,
	}

	runtime.EventsEmit(tm.app.Ctx, "onScheduledTasksLogRecord", logRecord)
	log.Printf("[TaskManager] Task [%s] completed. Results: %v", task.Name, results)
}

func (tm *TaskManager) saveTasks() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tasksPath := resolvePath("data/scheduledtasks.yaml")
	data, err := yaml.Marshal(tm.tasks)
	if err != nil {
		log.Printf("[TaskManager] Marshal tasks error: %v", err)
		return
	}

	err = os.WriteFile(tasksPath, data, 0644)
	if err != nil {
		log.Printf("[TaskManager] Save tasks error: %v", err)
	}
}

func (tm *TaskManager) readSubscriptions() ([]Subscription, error) {
	subsPath := resolvePath("data/subscribes.yaml")
	data, err := os.ReadFile(subsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var subs []Subscription
	if err := yaml.Unmarshal(data, &subs); err != nil {
		return nil, err
	}
	return subs, nil
}

func (tm *TaskManager) saveSubscriptions(subs []Subscription) error {
	subsPath := resolvePath("data/subscribes.yaml")
	data, err := yaml.Marshal(subs)
	if err != nil {
		return err
	}
	return os.WriteFile(subsPath, data, 0644)
}

func (tm *TaskManager) getProxyAddr() string {
	userPath := resolvePath("data/user.yaml")
	data, err := os.ReadFile(userPath)
	if err != nil {
		return ""
	}

	var userConfig map[string]any
	if err := yaml.Unmarshal(data, &userConfig); err != nil {
		return ""
	}

	// 兼容处理 Pinia 序列化嵌套
	var appSettings map[string]any
	if appVal, ok := userConfig["app"]; ok {
		appSettings, _ = appVal.(map[string]any)
	} else {
		appSettings = userConfig
	}

	if appSettings == nil {
		return ""
	}

	mode, _ := appSettings["requestProxyMode"].(string)
	custom, _ := appSettings["customProxy"].(string)

	switch mode {
	case "None":
		return ""
	case "Custom":
		return custom
	case "Kernel":
		return "http://127.0.0.1:20112"
	}
	return ""
}

func (tm *TaskManager) updateSubscriptions(ids []string) []any {
	var results []any
	subs, err := tm.readSubscriptions()
	if err != nil {
		return append(results, map[string]any{"ok": false, "result": err.Error()})
	}

	proxyAddr := tm.getProxyAddr()
	var needSave = false

	for i := range subs {
		sub := &subs[i]
		var match = false
		for _, id := range ids {
			if sub.ID == id {
				match = true
				break
			}
		}
		if !match {
			continue
		}

		if sub.Disabled {
			results = append(results, map[string]any{
				"ok":     false,
				"result": fmt.Sprintf("Subscription [%s] is disabled", sub.Name),
			})
			continue
		}

		err := tm.doUpdateSubscription(sub, proxyAddr)
		if err != nil {
			results = append(results, map[string]any{
				"ok":     false,
				"result": fmt.Sprintf("Failed to update subscription [%s]: %v", sub.Name, err),
			})
		} else {
			results = append(results, map[string]any{
				"ok":     true,
				"result": fmt.Sprintf("Subscription [%s] updated successfully.", sub.Name),
			})
			needSave = true
			runtime.EventsEmit(tm.app.Ctx, "subscriptionChange", map[string]any{"id": sub.ID})
		}
	}

	if needSave {
		if err := tm.saveSubscriptions(subs); err != nil {
			log.Printf("[TaskManager] Save subscriptions error: %v", err)
		}
		runtime.EventsEmit(tm.app.Ctx, "subscriptionsChange", nil)
	}

	return results
}

func (tm *TaskManager) updateAllSubscriptions() []any {
	subs, err := tm.readSubscriptions()
	if err != nil {
		return []any{map[string]any{"ok": false, "result": err.Error()}}
	}

	var ids []string
	for _, sub := range subs {
		if !sub.Disabled {
			ids = append(ids, sub.ID)
		}
	}
	return tm.updateSubscriptions(ids)
}

func (tm *TaskManager) doUpdateSubscription(sub *Subscription, defaultProxy string) error {
	if sub.Type != "Http" {
		return nil
	}

	headers := make(map[string]string)
	for k, v := range sub.Header.Request {
		headers[k] = v
	}

	proxy := defaultProxy
	switch sub.RequestProxyMode {
	case "None":
		proxy = ""
	case "Custom":
		proxy = sub.CustomProxy
	case "Kernel":
		proxy = "http://127.0.0.1:20112"
	}

	res := tm.app.Requests(
		sub.RequestMethod,
		sub.URL,
		headers,
		"",
		RequestOptions{
			Proxy:    proxy,
			Insecure: sub.InSecure,
			Timeout:  sub.RequestTimeout,
		},
	)

	if !res.Flag {
		return errors.New(res.Body)
	}

	if res.Status >= 400 {
		return fmt.Errorf("HTTP status code %d", res.Status)
	}

	// 流量与过期解析
	for k, v := range res.Headers {
		if strings.ToLower(k) == "subscription-userinfo" && len(v) > 0 {
			parts := strings.Split(v[0], ";")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				kv := strings.Split(part, "=")
				if len(kv) == 2 {
					val, _ := strconv.ParseInt(kv[1], 10, 64)
					switch kv[0] {
					case "upload":
						sub.Upload = val
					case "download":
						sub.Download = val
					case "total":
						sub.Total = val
					case "expire":
						sub.Expire = val * 1000
					}
				}
			}
		}
	}

	yamlText := res.Body
	if decoded, err := base64.StdEncoding.DecodeString(res.Body); err == nil {
		yamlText = string(decoded)
	}

	var doc map[string]any
	if err := yaml.Unmarshal([]byte(yamlText), &doc); err == nil {
		if proxiesVal, ok := doc["proxies"]; ok {
			if proxiesList, ok := proxiesVal.([]any); ok {
				var filteredProxies []any
				var proxyItems []ProxyItem

				for _, p := range proxiesList {
					if pMap, ok := p.(map[string]any); ok {
						name, _ := pMap["name"].(string)
						pType, _ := pMap["type"].(string)

						if name != "" {
							if sub.Include != "" {
								if matched, _ := regexp.MatchString(sub.Include, name); !matched {
									continue
								}
							}
							if sub.Exclude != "" {
								if matched, _ := regexp.MatchString(sub.Exclude, name); matched {
									continue
								}
							}

							filteredProxies = append(filteredProxies, pMap)
							proxyItems = append(proxyItems, ProxyItem{
								ID:   strconv.FormatInt(time.Now().UnixNano(), 10) + "_" + name,
								Name: name,
								Type: pType,
							})
						}
					}
				}
				doc["proxies"] = filteredProxies
				sub.Proxies = proxyItems

				if updatedData, err := yaml.Marshal(doc); err == nil {
					yamlText = string(updatedData)
				}
			}
		}
	}

	filePath := resolvePath(sub.Path)
	if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
		return err
	}

	err := os.WriteFile(filePath, []byte(yamlText), 0644)
	if err != nil {
		return err
	}

	sub.UpdateTime = time.Now().UnixMilli()
	return nil
}

func (tm *TaskManager) readRulesets() ([]Ruleset, error) {
	rulesPath := resolvePath("data/rulesets.yaml")
	data, err := os.ReadFile(rulesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var rules []Ruleset
	if err := yaml.Unmarshal(data, &rules); err != nil {
		return nil, err
	}
	return rules, nil
}

func (tm *TaskManager) saveRulesets(rules []Ruleset) error {
	rulesPath := resolvePath("data/rulesets.yaml")
	data, err := yaml.Marshal(rules)
	if err != nil {
		return err
	}
	return os.WriteFile(rulesPath, data, 0644)
}

func (tm *TaskManager) updateRulesets(ids []string) []any {
	var results []any
	rules, err := tm.readRulesets()
	if err != nil {
		return append(results, map[string]any{"ok": false, "result": err.Error()})
	}

	proxyAddr := tm.getProxyAddr()
	var needSave = false

	for i := range rules {
		rule := &rules[i]
		var match = false
		for _, id := range ids {
			if rule.ID == id {
				match = true
				break
			}
		}
		if !match {
			continue
		}

		if rule.Disabled {
			results = append(results, map[string]any{
				"ok":     false,
				"result": fmt.Sprintf("Ruleset [%s] is disabled", rule.Name),
			})
			continue
		}

		err := tm.doUpdateRuleset(rule, proxyAddr)
		if err != nil {
			results = append(results, map[string]any{
				"ok":     false,
				"result": fmt.Sprintf("Failed to update ruleset [%s]: %v", rule.Name, err),
			})
		} else {
			results = append(results, map[string]any{
				"ok":     true,
				"result": fmt.Sprintf("Ruleset [%s] updated successfully.", rule.Name),
			})
			needSave = true
			runtime.EventsEmit(tm.app.Ctx, "rulesetChange", map[string]any{"id": rule.ID})
		}
	}

	if needSave {
		if err := tm.saveRulesets(rules); err != nil {
			log.Printf("[TaskManager] Save rulesets error: %v", err)
		}
		runtime.EventsEmit(tm.app.Ctx, "rulesetsChange", nil)
	}

	return results
}

func (tm *TaskManager) updateAllRulesets() []any {
	rules, err := tm.readRulesets()
	if err != nil {
		return []any{map[string]any{"ok": false, "result": err.Error()}}
	}

	var ids []string
	for _, rule := range rules {
		if !rule.Disabled {
			ids = append(ids, rule.ID)
		}
	}
	return tm.updateRulesets(ids)
}

func (tm *TaskManager) doUpdateRuleset(rule *Ruleset, defaultProxy string) error {
	if rule.Type != "Http" {
		return nil
	}

	res := tm.app.Requests(
		"GET",
		rule.URL,
		nil,
		"",
		RequestOptions{
			Proxy:    defaultProxy,
			Insecure: false,
			Timeout:  15,
		},
	)

	if !res.Flag {
		return errors.New(res.Body)
	}

	if res.Status >= 400 {
		return fmt.Errorf("HTTP status code %d", res.Status)
	}

	// 规则数量计数
	var count = 0
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(res.Body), &doc); err == nil {
		if payloadVal, ok := doc["payload"]; ok {
			if payloadList, ok := payloadVal.([]any); ok {
				count = len(payloadList)
			}
		}
	}

	filePath := resolvePath(rule.Path)
	if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
		return err
	}

	err := os.WriteFile(filePath, []byte(res.Body), 0644)
	if err != nil {
		return err
	}

	rule.Count = count
	rule.UpdateTime = time.Now().UnixMilli()
	return nil
}

func (tm *TaskManager) updatePlugins(_ []string) []any {
	return []any{map[string]any{"ok": false, "result": "Plugin updates in backend not implemented."}}
}

func (tm *TaskManager) updateAllPlugins() []any {
	return []any{map[string]any{"ok": false, "result": "Plugin updates in backend not implemented."}}
}
