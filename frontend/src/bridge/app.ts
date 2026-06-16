import { apiRequest } from './request'

import type { AppEnv } from '@/types/app'

export const RestartApp = async () => {
  const { flag, data } = await apiRequest('app/restart')
  if (!flag) throw data
  return data
}

export const ExitApp = async () => {
  const { flag, data } = await apiRequest('app/exit')
  if (!flag) throw data
  return data
}

export const ShowMainWindow = async () => {}
export const UpdateTray = async () => {}
export const UpdateTrayMenus = async () => {}
export const UpdateTrayAndMenus = async (...args: any[]) => {}

export const GetEnv = async <T extends string | undefined = undefined>(
  key?: T,
): Promise<T extends string ? string : AppEnv> => {
  const res = await apiRequest('app/env', { key: key || '' })
  return res.data
}

export const IsStartup = async () => {
  const { flag, data } = await apiRequest('app/isstartup')
  if (!flag) throw data
  return data === 'true'
}

export const GetInterfaces = async () => {
  const { flag, data } = await apiRequest('app/interfaces')
  if (!flag) {
    throw data
  }
  return data.split('|')
}

export const Notify = async (title: string, body: string) => {
  console.log(`[Notification] ${title}: ${body}`)
}

export const WindowReloadApp = () => {
  window.location.reload()
}

export const WindowIsMaximised = async () => false
export const WindowIsMinimised = async () => false
export const WindowSetSystemDefaultTheme = async () => {}
export const WindowToggleMaximise = async () => {}
export const WindowMinimise = async () => {}
export const WindowUnminimise = async () => {}
export const WindowMaximise = async () => {}
export const WindowUnmaximise = async () => {}
export const WindowClose = async () => {}

export const ClipboardSetText = async (text: string) => {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch (e) {
    console.error('Failed to write clipboard:', e)
    return false
  }
}

export const ClipboardGetText = async () => {
  try {
    return await navigator.clipboard.readText()
  } catch (e) {
    console.error('Failed to read clipboard:', e)
    return ''
  }
}

export const BrowserOpenURL = async (url: string) => {
  window.open(url, '_blank')
}

export const WindowSetAlwaysOnTop = async (b: boolean) => {}
export const WindowHide = async () => {}
export const WindowSetSize = async (w: number, h: number) => {}

export const IsNotificationAvailable = async () => false
export const RequestNotificationAuthorization = async () => false



