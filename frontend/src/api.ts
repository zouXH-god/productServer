import { toast } from './toast'

export const session = {
  get token() { return sessionStorage.getItem('token') || '' },
  set token(value: string) { value ? sessionStorage.setItem('token', value) : sessionStorage.removeItem('token') }
}

export async function api<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers)
  if (session.token) headers.set('Authorization', `Bearer ${session.token}`)
  if (options.body && !(options.body instanceof FormData)) headers.set('Content-Type', 'application/json')
  const response = await fetch(path, { ...options, headers })
  if (response.status === 401) {
    session.token = ''
    if (location.pathname !== '/login') location.assign('/login')
  }
  if (!response.ok) {
    const data = await response.json().catch(() => ({}))
    const message = data?.error?.message || `请求失败 (${response.status})`
    toast(message, 'error', 4200)
    throw new Error(message)
  }
  const method = (options.method || 'GET').toUpperCase()
  if (method !== 'GET' && method !== 'HEAD') {
    const message = method === 'DELETE' ? '删除成功' : method === 'PUT' || method === 'PATCH' ? '保存成功' : path.includes('/test') ? '测试成功' : '操作成功'
    toast(message)
  }
  return response.status === 204 ? undefined as T : response.json()
}

export function authDownload(path: string, filename: string) {
  fetch(path, { headers: { Authorization: `Bearer ${session.token}` } }).then(async r => {
    if (!r.ok) throw new Error('下载失败')
    const url = URL.createObjectURL(await r.blob())
    const a = document.createElement('a'); a.href = url; a.download = filename; a.click(); URL.revokeObjectURL(url)
    toast('下载已开始')
  }).catch(e => toast(e.message, 'error'))
}
