package socketio

import "websocket/socketio/util"

// 简易安全的池子

type safePool struct {
	conn util.SyncMap[string, ws]
}

func (p *safePool) set(ws ws) {
	p.conn.Store(ws.GetUUID(), ws)
}

func (p *safePool) all() map[string]ws {
	ret := make(map[string]ws)
	p.conn.Range(func(wsUUID string, kws ws) bool {
		ret[wsUUID] = kws
		return true
	})
	return ret
}

func (p *safePool) get(key string) (ws, error) {
	ret, ok := p.conn.Load(key)
	if !ok {
		return nil, ErrorInvalidConnection
	}
	return ret, nil
}

func (p *safePool) contains(key string) bool {
	return p.conn.Exists(key)
}

func (p *safePool) delete(key string) {
	p.conn.Delete(key)
}
