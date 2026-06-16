import { apiRequest, EventsOn, EventsOff } from './request'
import { sampleID } from '@/utils'

interface ExecOptions {
  PidFile?: string
  LogFile?: string
  Convert?: boolean
  Env?: Record<string, any>
  StopOutputKeyword?: string
  WorkingDirectory?: string
  convert?: boolean
  env?: Record<string, any>
  stopOutputKeyword?: string
}

const mergeExecOptions = (options: ExecOptions) => {
  return {
    PidFile: options.PidFile ?? '',
    LogFile: options.LogFile ?? '',
    Convert: options.Convert ?? options.convert ?? false,
    Env: options.Env ?? options.env ?? {},
    StopOutputKeyword: options.StopOutputKeyword ?? options.stopOutputKeyword ?? '',
    WorkingDirectory: options.WorkingDirectory ?? '',
  }
}

export const Exec = async (path: string, args: string[], options: ExecOptions = {}) => {
  const { flag, data } = await apiRequest('exec/run', { path, args, options: mergeExecOptions(options) })
  if (!flag) {
    throw data
  }
  return data
}

export const ExecBackground = async (
  path: string,
  args: string[] = [],
  onOut?: (out: string) => void,
  onEnd?: (out: string) => void,
  options: ExecOptions = {},
) => {
  const outEvent = (onOut && sampleID()) || ''
  const endEvent = (onEnd && sampleID()) || (outEvent && sampleID()) || ''

  const { flag, data } = await apiRequest('exec/run-bg', {
    path,
    args,
    outEvent,
    endEvent,
    options: mergeExecOptions(options),
  })
  if (!flag) {
    throw data
  }

  if (outEvent) {
    EventsOn(outEvent, onOut!)
  }

  if (endEvent) {
    EventsOn(endEvent, (data: any) => {
      outEvent && EventsOff(outEvent)
      EventsOff(endEvent)
      onEnd?.(data)
    })
  }

  return Number(data)
}

export const ProcessInfo = async (pid: number) => {
  const { flag, data } = await apiRequest('exec/info', { pid })
  if (!flag) {
    throw data
  }
  return data
}

export const ProcessMemory = async (pid: number) => {
  const { flag, data } = await apiRequest('exec/memory', { pid })
  if (!flag) {
    throw data
  }
  return Number(data)
}

export const KillProcess = async (pid: number, timeout = 10) => {
  const { flag, data } = await apiRequest('exec/kill', { pid, timeout })
  if (!flag) {
    throw data
  }
  return data
}

export const ProbeAPI = async (url: string, secret = '') => {
  const { flag, data } = await apiRequest('exec/probe', { url, secret })
  return !!flag
}
