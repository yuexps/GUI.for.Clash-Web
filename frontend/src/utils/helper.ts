import { parse } from 'yaml'

import { deleteConnection, getConnections, useProxy } from '@/api/kernel'
import {
  Exec,
  ExitApp,
  ReadFile,
  WindowReloadApp,
  WriteFile,
} from '@/bridge'
import { CoreWorkingDirectory } from '@/constant/kernel'
import { OS, RequestProxyMode } from '@/enums/app'
import { ProxyGroupType, RulesetBehavior, RulesetFormat } from '@/enums/kernel'
import i18n from '@/lang'
import {
  type ProxyType,
  useAppSettingsStore,
  useAppStore,
  useEnvStore,
  useKernelApiStore,
  usePluginsStore,
  useRulesetsStore,
} from '@/stores'
import {
  formatProxyHost,
  ignoredError,
  normalizeRequestProxy,
  stringifyNoFolding,
  message,
  confirm,
} from '@/utils'

// Permissions Helper
export const SwitchPermissions = async (enable: boolean) => {
  const { appPath } = useEnvStore().env
  const args = enable
    ? [
        'add',
        'HKEY_CURRENT_USER\\Software\\Microsoft\\Windows NT\\CurrentVersion\\AppCompatFlags\\Layers',
        '/v',
        appPath,
        '/t',
        'REG_SZ',
        '/d',
        'RunAsAdmin',
        '/f',
      ]
    : [
        'delete',
        'HKEY_CURRENT_USER\\Software\\Microsoft\\Windows NT\\CurrentVersion\\AppCompatFlags\\Layers',
        '/v',
        appPath,
        '/f',
      ]
  await Exec('reg', args, { Convert: true })
}

export const CheckPermissions = async () => {
  const { appPath } = useEnvStore().env
  try {
    const out = await Exec(
      'reg',
      [
        'query',
        'HKEY_CURRENT_USER\\Software\\Microsoft\\Windows NT\\CurrentVersion\\AppCompatFlags\\Layers',
        '/v',
        appPath,
        '/t',
        'REG_SZ',
      ],
      { Convert: true },
    )
    return out.includes('RunAsAdmin')
  } catch {
    return false
  }
}

export const GrantTUNPermission = async (path: string) => {
  console.log('[Permission] Skip granting TUN permission in Web mode:', path)
}

export const RunWithOsaScript = async (
  path: string,
  args: string[] = [],
  options: { admin?: boolean; wait?: boolean } = {},
) => {
  const { admin = false, wait = true, ...others } = options
  const escapedArgs = args.map((arg) => `'${arg.replace(/'/g, "'\\''")}'`).join(' ')
  let shellCmd = `${path} ${escapedArgs}`.trim()
  if (!wait) {
    shellCmd += ' > /dev/null 2>&1 &'
  }
  const escapedShellCmd = shellCmd.replace(/\\/g, '\\\\').replace(/"/g, '\\"')
  let appleScript = `do shell script "${escapedShellCmd}"`
  if (admin) {
    appleScript += ' with administrator privileges'
  }
  const osaArgs = ['-e', appleScript]
  return await Exec('osascript', osaArgs, others)
}

export const RunWithPowerShell = async (
  path: string,
  args: string[] = [],
  options: { admin?: boolean; hidden?: boolean; wait?: boolean } = {},
) => {
  const { admin = false, hidden = false, wait = true, ...others } = options
  const psArgs: string[] = []
  let command = `Start-Process -FilePath "${path}"`
  if (args.length > 0) {
    const argList = args.map((a) => `"${a.replace(/"/g, '""')}"`).join(',')
    command += ` -ArgumentList ${argList}`
  }
  if (admin) {
    command += ' -Verb RunAs'
  }
  if (hidden) {
    command += ' -WindowStyle Hidden'
  }
  if (wait) {
    command += ' -Wait'
  }
  psArgs.push('-NoProfile', '-Command', command)
  return await Exec('powershell', psArgs, { Convert: true, ...others })
}

// SystemProxy Helper
export const SetSystemProxy = async (
  enable: boolean,
  server: string,
  proxyType: ProxyType = 'mixed',
  bypass = '',
) => {
  console.log('[SystemProxy] Skip setting system proxy on Web mode:', enable, server, proxyType)
}



export const GetSystemProxy = async () => {
  return ''
}

export const GetSystemProxyBypass = async () => {
  return ''
}

const requestProxyCache: { proxyPromise: Promise<string> | null; lastAccessTime: number } = {
  proxyPromise: null,
  lastAccessTime: 0,
}

export const GetRequestProxy = async (mode?: RequestProxyMode, customProxy?: string) => {
  const appSettings = useAppSettingsStore()
  const requestProxyMode = mode ?? appSettings.app.requestProxyMode

  if (requestProxyMode === RequestProxyMode.None) {
    return ''
  }

  if (requestProxyMode === RequestProxyMode.Kernel) {
    const kernelProxy = useKernelApiStore().getProxyEndpoint()
    if (!kernelProxy) return ''

    const { schema, host, port, username, password } = kernelProxy
    const formattedHost = formatProxyHost(host)
    const encodedUsername = encodeURIComponent(username)
    const encodedPassword = password ? `:${encodeURIComponent(password)}` : ''
    const auth = username || password ? `${encodedUsername}${encodedPassword}@` : ''

    return `${schema}://${auth}${formattedHost}:${port}`
  }

  if (requestProxyMode === RequestProxyMode.Custom) {
    return normalizeRequestProxy(customProxy ?? appSettings.app.customProxy)
  }

  if (requestProxyCache.proxyPromise && Date.now() - requestProxyCache.lastAccessTime < 1000) {
    return requestProxyCache.proxyPromise
  }

  requestProxyCache.lastAccessTime = Date.now()
  requestProxyCache.proxyPromise = GetSystemProxy()
  return requestProxyCache.proxyPromise
}

// Auto-start


export const IsAutoStartEnabled = async () => {
  return false
}

export const EnableAutoStart = async (delay = 10) => {
  // Web 模式不需要宿主机开机自启
}

export const DisableAutoStart = async () => {
  // Web 模式不需要宿主机开机自启
}

// Others
export const handleUseProxy = async (group: any, proxy: any) => {
  if (
    ![ProxyGroupType.Selector, ProxyGroupType.Fallback, ProxyGroupType.UrlTest].includes(group.type)
  ) {
    return
  }

  if (group.now === proxy.name) return
  const promises: Promise<null>[] = []
  const appSettings = useAppSettingsStore()
  const kernelApiStore = useKernelApiStore()
  if (appSettings.app.kernel.autoClose) {
    const { connections } = await getConnections()
    promises.push(
      ...(connections || [])
        .filter((v) => v.chains.includes(group.name))
        .map((v) => deleteConnection(v.id)),
    )
  }
  await useProxy(encodeURIComponent(group.name), proxy.name)
  await Promise.all(promises)
  await kernelApiStore.refreshProviderProxies()
}

export const handleChangeMode = async (mode: 'direct' | 'global' | 'rule') => {
  const kernelApiStore = useKernelApiStore()

  if (mode === kernelApiStore.config.mode) return

  kernelApiStore.updateConfig({ mode })

  const { connections } = await getConnections()
  const promises = (connections || []).map((v) => deleteConnection(v.id))
  await Promise.all(promises)
}

export const addToRuleSet = async (id: 'direct' | 'reject' | 'proxy', payloads: string[]) => {
  const path = `data/rulesets/${id}.yaml`

  const rulesetsStoe = useRulesetsStore()
  let ruleset = rulesetsStoe.getRulesetById(id)
  if (!ruleset) {
    ruleset = {
      id,
      name: id,
      updateTime: 0,
      type: 'Manual',
      behavior: RulesetBehavior.Classical,
      format: RulesetFormat.Yaml,
      url: '',
      path,
      count: 0,
      disabled: false,
    }
    await rulesetsStoe.addRuleset(ruleset)
  }

  const content = (await ignoredError(ReadFile, path)) || '{}'
  const { payload = [] } = parse(content)
  payload.unshift(...payloads)
  await WriteFile(path, stringifyNoFolding({ payload: [...new Set(payload)] }))
  await rulesetsStoe.updateRuleset(id)
}

export const reloadApp = async () => {
  const { t } = i18n.global
  const appStore = useAppStore()
  const pluginsStore = usePluginsStore()

  appStore.isAppReloading = true

  let timedout = false
  const { destroy } = message.info('titlebar.reloadPending', 10 * 60 * 1000)

  const timeoutId = setTimeout(async () => {
    timedout = true
    appStore.isAppReloading = false
    destroy()
    confirm('Warning', t('titlebar.reloadTimeout')).then(WindowReloadApp)
  }, 10_000)

  try {
    await pluginsStore.onReloadTrigger()
    if (!timedout) {
      clearTimeout(timeoutId)
      WindowReloadApp()
    }
  } catch (err: any) {
    clearTimeout(timeoutId)
    confirm('Error', t('titlebar.reloadError', { reason: err })).then(WindowReloadApp)
  }

  appStore.isAppReloading = false
  destroy()
}

export const exitApp = async () => {
  const { t } = i18n.global
  const appStore = useAppStore()
  const envStore = useEnvStore()
  const pluginsStore = usePluginsStore()
  const appSettings = useAppSettingsStore()
  const kernelApiStore = useKernelApiStore()

  appStore.isAppExiting = true

  let timedout = false
  const { destroy } = message.info('titlebar.exitPending', 10 * 60 * 1000)

  const timeoutId = setTimeout(async () => {
    timedout = true
    appStore.isAppExiting = false
    destroy()
    confirm('Warning', t('titlebar.exitTimeout')).then(ExitApp)
  }, 10_000)

  try {
    if (kernelApiStore.running && appSettings.app.closeKernelOnExit) {
      await kernelApiStore.stopCore()
      if (appSettings.app.autoSetSystemProxy) {
        await envStore.clearSystemProxy()
      }
    }
    await pluginsStore.onShutdownTrigger()
    if (!timedout) {
      clearTimeout(timeoutId)
      ExitApp()
    }
  } catch (err: any) {
    clearTimeout(timeoutId)
    confirm('Error', t('titlebar.exitError', { reason: err })).then(ExitApp)
  }

  appStore.isAppExiting = false
  destroy()
}

export const getKernelFileName = (isAlpha = false) => {
  const envStore = useEnvStore()
  const { os } = envStore.env
  const fileSuffix = { [OS.Windows]: '.exe', [OS.Linux]: '', [OS.Darwin]: '' }[os]
  const alpha = isAlpha ? '-alpha' : ''
  return `mihomo${alpha}${fileSuffix}`
}

export const getKernelAssetFileName = (version: string, cpuLevel: 'v1' | 'v2' | 'v3' = 'v3') => {
  const envStore = useEnvStore()
  const { os, arch } = envStore.env
  const cpuLevelFlag = arch === 'amd64' ? `-${cpuLevel}` : ''
  const suffix = { [OS.Windows]: '.zip', [OS.Linux]: '.gz', [OS.Darwin]: '.gz' }[os]
  return `mihomo-${os}-${arch}${cpuLevelFlag}-${version}${suffix}`
}

export const processMagicVariables = (str: string) => {
  const { env } = useEnvStore()
  let result = str
  Object.entries({
    $APP_BASE_PATH: env.basePath,
    $CORE_BASE_PATH: CoreWorkingDirectory,
  }).forEach(([source, target]) => {
    result = result.replaceAll(source, target)
  })
  return result
}

export const getKernelRuntimeEnv = (isAlpha = false) => {
  const appSettings = useAppSettingsStore()
  const { env } = isAlpha ? appSettings.app.kernel.alpha : appSettings.app.kernel.main
  return Object.entries(env).reduce((p, [key, value]) => {
    p[key] = processMagicVariables(value)
    return p
  }, {} as Recordable)
}

export const getKernelRuntimeArgs = (isAlpha = false) => {
  const appSettings = useAppSettingsStore()
  const { args } = isAlpha ? appSettings.app.kernel.alpha : appSettings.app.kernel.main
  return args.map((arg) => processMagicVariables(arg))
}
