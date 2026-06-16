import { apiRequest } from './request'

interface IOOptions {
  Mode?: 'Binary' | 'Text'
  Range?: string
}

export const WriteFile = async (path: string, content: string, options: IOOptions = {}) => {
  const { flag, data } = await apiRequest('io/write', { path, content, options: { Mode: 'Text', Range: '', ...options } })
  if (!flag) {
    throw data
  }
  return data
}

export const ReadFile = async (path: string, options: IOOptions = {}) => {
  const { flag, data } = await apiRequest('io/read', { path, options: { Mode: 'Text', Range: '', ...options } })
  if (!flag) {
    throw data
  }
  return data
}

export const MoveFile = async (source: string, target: string) => {
  const { flag, data } = await apiRequest('io/move', { source, target })
  if (!flag) {
    throw data
  }
  return data
}

export const RemoveFile = async (path: string) => {
  const { flag, data } = await apiRequest('io/remove', { path })
  if (!flag) {
    throw data
  }
  return data
}

export const CopyFile = async (source: string, target: string) => {
  const { flag, data } = await apiRequest('io/copy', { source, target })
  if (!flag) {
    throw data
  }
  return data
}

export const FileExists = async (path: string) => {
  const { flag, data } = await apiRequest('io/exists', { path })
  if (!flag) {
    throw data
  }
  return data === 'true'
}

export const AbsolutePath = async (path: string) => {
  const { flag, data } = await apiRequest('io/absolute', { path })
  if (!flag) {
    throw data
  }
  return data
}

export const MakeDir = async (path: string) => {
  const { flag, data } = await apiRequest('io/mkdir', { path })
  if (!flag) {
    throw data
  }
  return data
}

export const ReadDir = async (path: string) => {
  const { flag, data } = await apiRequest('io/readdir', { path })
  if (!flag) {
    throw data
  }
  return data
    .split('|')
    .filter((v: string) => v)
    .map((v: string) => {
      const [name, size, isDir] = v.split(',') as [string, string, string]
      return { name, size: Number(size), isDir: isDir === 'true' }
    })
}

export const OpenDir = async (path: string) => {
  const { flag, data } = await apiRequest('io/opendir', { path })
  if (!flag) {
    throw data
  }
  return data
}

export const OpenURI = async (uri: string) => {
  const { flag, data } = await apiRequest('io/openuri', { uri })
  if (!flag) {
    throw data
  }
  return data
}

export const UnzipZIPFile = async (path: string, output: string) => {
  const { flag, data } = await apiRequest('io/unzip', { path, output })
  if (!flag) {
    throw data
  }
  return data
}

export const UnzipGZFile = async (path: string, output: string) => {
  const { flag, data } = await apiRequest('io/unzipgz', { path, output })
  if (!flag) {
    throw data
  }
  return data
}

export const UnzipTarGZFile = async (path: string, output: string) => {
  const { flag, data } = await apiRequest('io/unziptargz', { path, output })
  if (!flag) {
    throw data
  }
  return data
}
