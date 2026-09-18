import { useCallback, useEffect, useState } from 'react'
import { api, ConnectionInfo, LogEntry, RegisterDef } from './api'

type Poll = ReturnType<typeof setInterval> | undefined

export default function App() {
  const [connections, setConnections] = useState<ConnectionInfo[]>([])
  const [selected, setSelected] = useState<string | null>(null)
  const [regs, setRegs] = useState<RegisterDef[]>([])
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [error, setError] = useState<string | null>(null)
  const [mutationRunning, setMutationRunning] = useState(false)

  const refreshConnections = useCallback(async () => {
    try {
      setConnections(await api.listConnections())
      setError(null)
    } catch (e) {
      setError(String(e))
    }
  }, [])

  const refreshRegisters = useCallback(async (id: string) => {
    try {
      setRegs(await api.listRegisters(id, 1))
    } catch {
      setRegs([])
    }
  }, [])

  const refreshLogs = useCallback(async (id: string) => {
    try {
      setLogs((await api.getLogs(id)).slice(-200).reverse())
    } catch {
      setLogs([])
    }
  }, [])

  // Poll state every second.
  useEffect(() => {
    const timer: Poll = setInterval(() => {
      refreshConnections()
      if (selected) {
        refreshRegisters(selected)
        refreshLogs(selected)
      }
    }, 1000)
    return () => clearInterval(timer)
  }, [refreshConnections, refreshRegisters, refreshLogs, selected])

  // Initial load.
  useEffect(() => {
    refreshConnections()
  }, [refreshConnections])

  const selectConn = (id: string) => {
    setSelected(id)
    refreshRegisters(id)
    refreshLogs(id)
  }

  const createConnection = async () => {
    try {
      const info = await api.createConnection({
        slave_id: 1,
        name: 'Simulator 1',
        transport: { type: 'tcp', host: '127.0.0.1', port: 5020 },
      })
      await refreshConnections()
      setSelected(info.id)
    } catch (e) {
      setError(String(e))
    }
  }

  const toggleConnection = async (conn: ConnectionInfo) => {
    try {
      if (conn.state === 'Running') {
        await api.stopConnection(conn.id)
      } else {
        await api.startConnection(conn.id)
      }
      await refreshConnections()
    } catch (e) {
      setError(String(e))
    }
  }

  const removeConnection = async (id: string) => {
    try {
      await api.deleteConnection(id)
      if (selected === id) setSelected(null)
      await refreshConnections()
    } catch (e) {
      setError(String(e))
    }
  }

  const toggleMutation = async () => {
    try {
      const res = await api.setMutationRunning(!mutationRunning)
      setMutationRunning(res.running)
    } catch (e) {
      setError(String(e))
    }
  }

  const selectedConn = connections.find((c) => c.id === selected)

  return (
    <div className="app">
      <header>
        <h1>ModbusSim <span className="go-badge">Go</span></h1>
        <button className={mutationRunning ? 'on' : ''} onClick={toggleMutation}>
          变位 {mutationRunning ? '运行中' : '已停止'}
        </button>
      </header>

      {error && <div className="error">{error}</div>}

      <main>
        <section className="panel">
          <div className="panel-head">
            <h2>连接</h2>
            <button onClick={createConnection}>+ 新建子站</button>
          </div>
          <ul className="conn-list">
            {connections.map((c) => (
              <li
                key={c.id}
                className={c.id === selected ? 'selected' : ''}
                onClick={() => setSelected(c.id)}
              >
                <div>
                  <b>{c.id}</b>
                  <span className={c.state === 'Running' ? 'state running' : 'state'}>
                    {c.state === 'Running' ? '运行中' : c.state}
                  </span>
                </div>
                <div className="muted">
                  {c.bind_address}:{c.port} · {c.device_count} 设备
                </div>
                <div className="conn-actions">
                  <button onClick={(e) => { e.stopPropagation(); toggleConnection(c) }}>
                    {c.state === 'Running' ? '停止' : '启动'}
                  </button>
                  <button className="danger" onClick={(e) => { e.stopPropagation(); removeConnection(c.id) }}>
                    删除
                  </button>
                </div>
              </li>
            ))}
            {connections.length === 0 && <li className="muted">暂无连接，点击「新建子站」开始</li>}
          </ul>
        </section>

        {selectedConn && (
          <>
            <section className="panel">
              <div className="panel-head"><h2>寄存器（设备 1，前 50 点）</h2></div>
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>地址</th><th>类型</th><th>数据类型</th><th>名称</th>
                    </tr>
                  </thead>
                  <tbody>
                    {regs.slice(0, 50).map((r) => (
                      <tr key={r.register_type + r.address}>
                        <td>{r.address}</td>
                        <td>{r.register_type}</td>
                        <td>{r.data_type}</td>
                        <td>{r.name || '—'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>

            <section className="panel">
              <div className="panel-head">
                <h2>通信日志</h2>
                <button onClick={() => api.clearLogs(selectedConn.id).then(() => refreshLogs(selectedConn.id))}>
                  清空
                </button>
              </div>
              <div className="logs">
                {logs.slice(-50).map((e, i) => (
                  <div key={i} className={e.direction}>
                    <span className="muted">{new Date(e.timestamp).toLocaleTimeString()}</span>{' '}
                    <b>{e.direction.toUpperCase()}</b> {e.function_code} {e.detail}
                  </div>
                ))}
                {logs.length === 0 && <div className="muted">暂无日志（启动连接后由主站请求触发）</div>}
              </div>
            </section>
          </>
        )}
      </main>
    </div>
  )
}
