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
    throw new Error(data?.error?.message || `请求失败 (${response.status})`)
  }
  return response.status === 204 ? undefined as T : response.json()
}

export function authDownload(path: string, filename: string) {
  fetch(path, { headers: { Authorization: `Bearer ${session.token}` } }).then(async r => {
    if (!r.ok) throw new Error('下载失败')
    const url = URL.createObjectURL(await r.blob())
    const a = document.createElement('a'); a.href = url; a.download = filename; a.click(); URL.revokeObjectURL(url)
  }).catch(e => alert(e.message))
}
