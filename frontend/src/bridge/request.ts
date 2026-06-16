const eventCallbacks: Record<string, ((...args: any[]) => void)[]> = {}
let ws: WebSocket | null = null
let reconnectTimer: any = null

function getBasePath() {
  const path = window.location.pathname
  return path.endsWith('/') ? path : path + '/'
}

function getWsUrl() {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const host = window.location.host
  const basePath = getBasePath()
  return `${protocol}//${host}${basePath}ws/events`
}

export function initWebSocket() {
  if (ws) return
  ws = new WebSocket(getWsUrl())

  ws.onopen = () => {
    console.log('WS Connection established')
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
  }

  ws.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data)
      if (msg.event) {
        const callbacks = eventCallbacks[msg.event]
        if (callbacks) {
          const args = Array.isArray(msg.data) ? msg.data : [msg.data]
          callbacks.forEach((cb) => cb(...args))
        }
      }
    } catch (e) {
      console.error('WS message error:', e)
    }
  }

  ws.onclose = () => {
    console.log('WS Connection closed, retrying...')
    ws = null
    if (!reconnectTimer) {
      reconnectTimer = setTimeout(initWebSocket, 3000)
    }
  }

  ws.onerror = (err) => {
    console.error('WS Error:', err)
  }
}

export function apiRequest(endpoint: string, body: any = {}) {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }

  const basePath = getBasePath()
  return fetch(`${basePath}api/${endpoint}`, {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  })
    .then((res) => {
      if (!res.ok) {
        return res.text().then((text) => {
          throw new Error(text || `HTTP error! status: ${res.status}`)
        })
      }
      return res.json()
    })
}

// 模拟 Wails EventsOn
export function EventsOn(event: string, callback: (...args: any[]) => void) {
  initWebSocket()
  if (!eventCallbacks[event]) {
    eventCallbacks[event] = []
  }
  eventCallbacks[event].push(callback)
  return () => {
    EventsOff(event, callback)
  }
}

// 模拟 Wails EventsOff
export function EventsOff(event: string, callback?: (...args: any[]) => void) {
  if (!eventCallbacks[event]) return
  if (callback) {
    eventCallbacks[event] = eventCallbacks[event].filter((cb) => cb !== callback)
  } else {
    delete eventCallbacks[event]
  }
}

// 模拟 Wails EventsEmit
export function EventsEmit(event: string, ...data: any[]) {
  initWebSocket()
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ event, data }))
  } else {
    console.warn('WS not open, EventsEmit dropped:', event)
  }
}

// 计划任务后端 API
export function ReloadScheduledTasks() {
  return apiRequest('task/reload')
}

export function RunScheduledTask(id: string) {
  return apiRequest('task/run', { id })
}

