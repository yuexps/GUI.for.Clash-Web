import { apiRequest } from './request'

type QueryType =
  | 'ASN'
  | 'AnonymousIP'
  | 'City'
  | 'ConnectionType'
  | 'Country'
  | 'Domain'
  | 'Enterprise'

export const OpenMMDB = async (path: string, id: string) => {
  const { flag, data } = await apiRequest('mmdb/open', { path, id })
  if (!flag) {
    throw data
  }
  return {
    close: () => CloseMMDB(path, id),
    query: (ip: string, type: QueryType) => QueryMMDB(path, ip, type),
  }
}

export const CloseMMDB = async (path: string, id: string) => {
  const { flag, data } = await apiRequest('mmdb/close', { path, id })
  if (!flag) {
    throw data
  }
  return data
}

export const QueryMMDB = async (path: string, ip: string, type: QueryType = 'Country') => {
  const { flag, data } = await apiRequest('mmdb/query', { path, ip, type })
  if (!flag) {
    throw data
  }
  return JSON.parse(data)
}
