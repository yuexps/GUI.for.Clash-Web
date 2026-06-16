import { defineStore } from 'pinia'
import { ref } from 'vue'
import { parse } from 'yaml'

import { ReadFile, WriteFile, Notify, ReloadScheduledTasks, RunScheduledTask, EventsOn } from '@/bridge'
import { ScheduledTasksFilePath } from '@/constant/app'
import { ScheduledTasksType, PluginTriggerEvent } from '@/enums/app'
import { useSubscribesStore, useRulesetsStore, usePluginsStore, useLogsStore } from '@/stores'
import { ignoredError, stringifyNoFolding } from '@/utils'

import type { ScheduledTask } from '@/types/app'

export const useScheduledTasksStore = defineStore('scheduledtasks', () => {
  const scheduledtasks = ref<ScheduledTask[]>([])

  const setupScheduledTasks = async () => {
    const data = await ignoredError(ReadFile, ScheduledTasksFilePath)
    data && (scheduledtasks.value = parse(data))

    // 重新加载后端定时任务
    await ReloadScheduledTasks()

    // 监听后端执行日志，同步更新状态与触发通知
    EventsOn('onScheduledTasksLogRecord', async (logRecord: any) => {
      const logsStore = useLogsStore()
      logsStore.recordScheduledTasksLog(logRecord)

      // 重新拉取配置刷新上次运行时间
      const updatedData = await ignoredError(ReadFile, ScheduledTasksFilePath)
      updatedData && (scheduledtasks.value = parse(updatedData))

      // 弹出通知提示
      const task = scheduledtasks.value.find((v) => v.name === logRecord.name)
      if (task && task.notification && Array.isArray(logRecord.result)) {
        const successes = logRecord.result.filter((v: any) => v.ok).length
        const failures = logRecord.result.length - successes
        const details = logRecord.result.flatMap((v: any) => v.result).join('\n')
        const content = `Successes: ${successes}; Failures: ${failures}. \n\n${details}`
        Notify(task.name, content)
      }
    })
  }

  const runScheduledTask = async (id: string) => {
    await RunScheduledTask(id)
  }

  const withOutput = <T>(list: string[], fn: (id: string) => Promise<T>) => {
    return async () => {
      const output: { ok: boolean; result: T }[] = []
      for (const id of list) {
        try {
          const result = await fn(id)
          if (Array.isArray(result)) {
            output.push(...result)
          } else {
            output.push({ ok: true, result })
          }
        } catch (error: any) {
          output.push({ ok: false, result: error.message || error })
        }
      }
      return output
    }
  }

  const getTaskFn = (task: ScheduledTask) => {
    switch (task.type) {
      case ScheduledTasksType.UpdateSubscription: {
        const subscribesStore = useSubscribesStore()
        return withOutput(task.subscriptions, subscribesStore.updateSubscribe)
      }
      case ScheduledTasksType.UpdateRuleset: {
        const rulesetsStore = useRulesetsStore()
        return withOutput(task.rulesets, rulesetsStore.updateRuleset)
      }
      case ScheduledTasksType.UpdatePlugin: {
        const pluginsStores = usePluginsStore()
        return withOutput(task.plugins, pluginsStores.updatePlugin)
      }
      case ScheduledTasksType.UpdateAllSubscription: {
        const subscribesStore = useSubscribesStore()
        return withOutput(['0'], () => subscribesStore.updateSubscribes())
      }
      case ScheduledTasksType.UpdateAllRuleset: {
        const rulesetsStore = useRulesetsStore()
        return withOutput(['1'], () => rulesetsStore.updateRulesets())
      }
      case ScheduledTasksType.UpdateAllPlugin: {
        const pluginsStores = usePluginsStore()
        return withOutput(['2'], () => pluginsStores.updatePlugins())
      }
      case ScheduledTasksType.RunPlugin: {
        const pluginsStores = usePluginsStore()
        return withOutput(task.plugins, async (id: string) =>
          pluginsStores.manualTrigger(id, PluginTriggerEvent.OnTask),
        )
      }
      case ScheduledTasksType.RunScript: {
        return withOutput([task.script], (script: string) => new window.AsyncFunction(script)())
      }
    }
  }

  const saveScheduledTasks = () => {
    return WriteFile(ScheduledTasksFilePath, stringifyNoFolding(scheduledtasks.value))
  }

  const addScheduledTask = async (s: ScheduledTask) => {
    scheduledtasks.value.push(s)
    try {
      await saveScheduledTasks()
      await ReloadScheduledTasks()
    } catch (error) {
      const idx = scheduledtasks.value.indexOf(s)
      if (idx !== -1) {
        scheduledtasks.value.splice(idx, 1)
      }
      throw error
    }
  }

  const deleteScheduledTask = async (id: string) => {
    const idx = scheduledtasks.value.findIndex((v) => v.id === id)
    if (idx === -1) return
    const backup = scheduledtasks.value.splice(idx, 1)[0]!
    try {
      await saveScheduledTasks()
      await ReloadScheduledTasks()
    } catch (error) {
      scheduledtasks.value.splice(idx, 0, backup)
      throw error
    }
  }

  const editScheduledTask = async (id: string, s: ScheduledTask) => {
    const idx = scheduledtasks.value.findIndex((v) => v.id === id)
    if (idx === -1) return
    const backup = scheduledtasks.value.splice(idx, 1, s)[0]!
    try {
      await saveScheduledTasks()
      await ReloadScheduledTasks()
    } catch (error) {
      scheduledtasks.value.splice(idx, 1, backup)
      throw error
    }
  }

  const getScheduledTaskById = (id: string) => scheduledtasks.value.find((v) => v.id === id)

  return {
    scheduledtasks,
    setupScheduledTasks,
    saveScheduledTasks,
    addScheduledTask,
    editScheduledTask,
    deleteScheduledTask,
    getScheduledTaskById,
    getTaskFn,
    runScheduledTask,
  }
})
