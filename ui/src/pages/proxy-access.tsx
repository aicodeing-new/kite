import { useEffect, useState } from 'react'
import { useAuth } from '@/contexts/auth-context'
import { Link, useLocation } from 'react-router-dom'
import { withSubPath } from '@/lib/subpath'

interface Device { id: number; deviceName: string; createdAt: string; lastSeenAt: string }

export function ProxyAccessPage() {
  const { user, isLoading } = useAuth()
  const location = useLocation()
  const params = new URLSearchParams(location.search)
  const authorizing = location.pathname.endsWith('/authorize')
  const [devices, setDevices] = useState<Device[]>([])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  async function request(path: string, method = 'GET', body?: unknown) {
    const response = await fetch(withSubPath('/api/auth/proxy' + path), {
      method, credentials: 'include', headers: { 'Content-Type': 'application/json' },
      body: body ? JSON.stringify(body) : undefined,
    })
    if (!response.ok) throw new Error((await response.json().catch(() => ({}))).error || '操作失败')
    return response.status === 204 ? null : response.json()
  }
  useEffect(() => {
    if (user && !authorizing) request('/sessions').then(data => setDevices(data.sessions || [])).catch(err => setError(String(err)))
  }, [user, authorizing])
  async function authorize() {
    setBusy(true); setError('')
    try {
      const result = await request('/authorize', 'POST', {
        redirectURI: params.get('redirect_uri'), challenge: params.get('code_challenge'),
        state: params.get('state'), deviceName: params.get('device_name') || 'Windows',
      })
      const target = new URL(result.redirectURI)
      if (target.protocol !== 'http:' || target.hostname !== '127.0.0.1' || target.pathname !== '/callback') throw new Error('无效的客户端回调地址')
      window.location.assign(target.href)
    } catch (err) { setError(String(err)); setBusy(false) }
  }
  if (isLoading) return <div className="p-12 text-center">正在加载账号…</div>
  return <main className="mx-auto max-w-2xl px-6 py-16">
    <Link to="/" className="text-sm text-muted-foreground">← 返回 Kite</Link>
    <p className="mt-10 text-sm font-medium text-primary">KITE PROXY</p>
    <h1 className="my-3 text-3xl font-semibold">{authorizing ? '授权桌面客户端' : '已授权设备'}</h1>
    {!user ? <><p className="my-6">请先登录 Kite，再继续授权。</p><Link className="rounded-md bg-primary px-5 py-3 text-primary-foreground" to="/login" onClick={() => sessionStorage.setItem('kite.proxy.return', location.pathname + location.search)}>登录 Kite</Link></> : <>
      <p className="text-muted-foreground">当前账号：{user.username}</p>
      {authorizing ? <div className="my-8 space-y-6 rounded-xl border p-6">
        <div><p className="text-sm text-muted-foreground">设备</p><p className="mt-1 font-medium">{params.get('device_name') || 'Windows'}</p></div>
        <p>客户端将按你当前的代理权限获取集群配置，直接连接 Kubernetes 进行端口转发。配置仅加密缓存在客户端内存中。</p>
        <p className="text-sm text-muted-foreground">此授权不会授予额外权限。请仅授权你刚刚在本人电脑上发起的登录请求。你可以随时在“已授权设备”中撤销会话。</p>
        <div className="flex items-center gap-5"><button disabled={busy} onClick={authorize} className="rounded-md bg-primary px-5 py-2.5 text-primary-foreground disabled:opacity-50">{busy ? '正在授权…' : '授权此设备'}</button><Link to="/proxy/devices">取消</Link></div>
      </div> : <div className="mt-8 space-y-3">{devices.length === 0 && <p className="rounded-lg border p-6 text-muted-foreground">暂无已授权的设备。</p>}{devices.map(device => <div key={device.id} className="flex items-center justify-between gap-4 rounded-lg border p-5"><div><p className="font-medium">{device.deviceName}</p><p className="mt-1 text-sm text-muted-foreground">最近活动：{new Date(device.lastSeenAt).toLocaleString()}</p></div><button disabled={busy} className="text-sm text-destructive" onClick={async () => {
        if (!window.confirm('撤销此设备授权？客户端将在下一次校验时停止连接。')) return
        setBusy(true)
        try { await request('/sessions/' + device.id, 'DELETE'); setDevices(old => old.filter(item => item.id !== device.id)) } catch (err) { setError(String(err)) } finally { setBusy(false) }
      }}>撤销授权</button></div>)}</div>}
    </>}
    {error && <p role="alert" className="mt-6 rounded-lg bg-destructive/10 p-4 text-destructive">{error}</p>}
  </main>
}
