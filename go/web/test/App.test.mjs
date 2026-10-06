import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import React from 'react'
import { act, create } from 'react-test-renderer'
import ts from 'typescript'

const require = createRequire(import.meta.url)
// Compile the real component with the existing TypeScript dependency. Only its
// HTTP client is replaced; React state, effects, and click handlers all run.
const componentCode = ts.transpileModule(
  readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8'),
  {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2020,
      jsx: ts.JsxEmit.ReactJSX,
    },
  },
).outputText

function loadApp(api) {
  const module = { exports: {} }
  new Function('require', 'module', 'exports', componentCode)(
    (name) => name === './api' ? { api } : require(name),
    module,
    module.exports,
  )
  return module.exports.default
}

const connection = (id) => ({
  id, bind_address: '127.0.0.1', port: 5020, state: 'Stopped', device_count: 1,
})
const register = (name) => ({
  address: 0, register_type: 'Holding', data_type: 'UInt16', endian: 'Big', name, comment: '',
})
const log = (detail) => ({
  timestamp: '2026-01-01T00:00:00Z', direction: 'rx', function_code: '03', detail,
})

async function harness(t) {
  let connections = [connection('A'), connection('B')]
  const requests = []
  function request(action, id) {
    let resolve, reject
    const promise = new Promise((yes, no) => { resolve = yes; reject = no })
    requests.push({ action, id, promise, resolve, reject })
    return promise
  }
  const api = {
    listConnections: async () => connections,
    listRegisters: (id) => request('registers', id),
    getLogs: (id) => request('logs', id),
    createConnection: () => request('create'),
    deleteConnection: (id) => request('delete', id),
    clearLogs: (id) => request('clear', id),
  }
  const intervals = new Map()
  let nextTimer = 0
  const realSetInterval = globalThis.setInterval
  const realClearInterval = globalThis.clearInterval
  globalThis.setInterval = (callback) => {
    const id = ++nextTimer
    intervals.set(id, callback)
    return id
  }
  globalThis.clearInterval = (id) => intervals.delete(id)
  let renderer
  t.after(async () => {
    try {
      await act(async () => { renderer?.unmount() })
      assert.equal(intervals.size, 0, 'unmount clears the polling timer')
    } finally {
      globalThis.setInterval = realSetInterval
      globalThis.clearInterval = realClearInterval
    }
  })
  await act(async () => { renderer = create(React.createElement(loadApp(api))) })
  const findRequests = (action, id) => requests.filter((r) => r.action === action && r.id === id)
  const latest = (action, id) => {
    const result = findRequests(action, id).at(-1)
    assert.ok(result, `expected ${action} request for ${id}`)
    return result
  }
  const row = (id) => renderer.root.findAllByType('li').find((r) =>
    r.findAllByType('b').some((b) => b.children[0] === id))
  const click = async (node) => {
    await act(async () => { node.props.onClick({ stopPropagation() {} }) })
  }
  const tick = async () => {
    await act(async () => { for (const callback of [...intervals.values()]) callback() })
  }
  const settle = async (req, value, reject = false) => {
    await act(async () => { reject ? req.reject(value) : req.resolve(value) })
  }
  const details = () => JSON.stringify(renderer.toJSON())
  const select = async (id) => click(row(id))
  const showDetails = async (id, marker) => {
    await settle(latest('registers', id), [register(`${marker}-register`)])
    await settle(latest('logs', id), [log(`${marker}-log`)])
  }
  // Poll explicitly in race tests so they exercise the baseline's delayed fetch
  // too. The immediate-load tests below deliberately do not advance the clock.
  const selectAndPoll = async (id) => { await select(id); await tick() }
  return {
    requests, findRequests, latest, row, click, tick, settle, details, select,
    selectAndPoll, showDetails,
    setConnections: (value) => { connections = value },
    selected: () => renderer.root.findAllByType('li')
      .filter((r) => r.props.className === 'selected')
      .map((r) => r.findByType('b').children[0]),
    button: (text) => renderer.root.findAllByType('button')
      .find((b) => b.children.join('') === text),
  }
}

function assertDetails(h, marker) {
  assert.ok(h.details().includes(`${marker}-register`), `shows ${marker} registers`)
  assert.ok(h.details().includes(`${marker}-log`), `shows ${marker} logs`)
}

function assertNoDetails(h, marker) {
  assert.ok(!h.details().includes(`${marker}-register`), `does not show ${marker} registers`)
  assert.ok(!h.details().includes(`${marker}-log`), `does not show ${marker} logs`)
}

test('selecting a connection immediately requests its registers and logs', async (t) => {
  const h = await harness(t)
  await h.select('A')
  assert.deepEqual(h.selected(), ['A'])
  assert.equal(h.findRequests('registers', 'A').length, 1)
  assert.equal(h.findRequests('logs', 'A').length, 1)
  await h.showDetails('A', 'A')
  assertDetails(h, 'A')
})

test('switching selection clears the previous details before responses arrive', async (t) => {
  const h = await harness(t)
  await h.selectAndPoll('A')
  await h.showDetails('A', 'A')
  assertDetails(h, 'A')
  await h.select('B')
  assert.deepEqual(h.selected(), ['B'])
  assertNoDetails(h, 'A')
  assert.equal(h.findRequests('registers', 'B').length, 1)
  assert.equal(h.findRequests('logs', 'B').length, 1)
})

for (const reject of [false, true]) {
  test(`late ${reject ? 'failed' : 'successful'} requests cannot replace a newer selection`, async (t) => {
    const h = await harness(t)
    await h.selectAndPoll('A')
    const oldRegisters = h.latest('registers', 'A')
    const oldLogs = h.latest('logs', 'A')
    await h.selectAndPoll('B')
    await h.showDetails('B', 'B')
    await h.settle(oldRegisters, reject ? new Error('old request') : [register('A-register')], reject)
    await h.settle(oldLogs, reject ? new Error('old request') : [log('A-log')], reject)
    assert.deepEqual(h.selected(), ['B'])
    assertDetails(h, 'B')
    assertNoDetails(h, 'A')
  })
}

test('returning to a connection rejects responses from its earlier selection', async (t) => {
  const h = await harness(t)
  await h.selectAndPoll('A')
  const oldRegisters = h.latest('registers', 'A')
  const oldLogs = h.latest('logs', 'A')
  await h.selectAndPoll('B')
  await h.selectAndPoll('A')
  await h.showDetails('A', 'current-A')
  await h.settle(oldRegisters, [register('old-A-register')])
  await h.settle(oldLogs, [log('old-A-log')])
  assertDetails(h, 'current-A')
  assertNoDetails(h, 'old-A')
})

test('an older poll cannot overwrite a newer response for the same selection', async (t) => {
  const h = await harness(t)
  await h.selectAndPoll('A')
  const oldRegisters = h.latest('registers', 'A')
  const oldLogs = h.latest('logs', 'A')
  await h.tick()
  await h.showDetails('A', 'new')
  await h.settle(oldRegisters, [register('old-register')])
  await h.settle(oldLogs, new Error('old poll failed'), true)
  assertDetails(h, 'new')
  assertNoDetails(h, 'old')
})

test('deleting a former selection preserves a selection made while deletion was pending', async (t) => {
  const h = await harness(t)
  await h.selectAndPoll('A')
  await h.click(h.row('A').findByProps({ className: 'danger' }))
  await h.selectAndPoll('B')
  await h.showDetails('B', 'B')
  h.setConnections([connection('B')])
  await h.settle(h.latest('delete', 'A'), { ok: 'ok' })
  assert.deepEqual(h.selected(), ['B'])
  assertDetails(h, 'B')
})

test('deleting the current connection clears selection and invalidates pending details', async (t) => {
  const h = await harness(t)
  await h.selectAndPoll('A')
  const oldRegisters = h.latest('registers', 'A')
  const oldLogs = h.latest('logs', 'A')
  await h.click(h.row('A').findByProps({ className: 'danger' }))
  h.setConnections([connection('B')])
  await h.settle(h.latest('delete', 'A'), { ok: 'ok' })
  assert.deepEqual(h.selected(), [])
  await h.settle(oldRegisters, [register('deleted-register')])
  await h.settle(oldLogs, [log('deleted-log')])
  await h.select('B')
  assertNoDetails(h, 'deleted')
})

test('creation selects and immediately initializes the new connection with empty details', async (t) => {
  const h = await harness(t)
  await h.selectAndPoll('A')
  await h.showDetails('A', 'A')
  await h.click(h.button('+ 新建子站'))
  h.setConnections([connection('A'), connection('B'), connection('C')])
  await h.settle(h.latest('create'), connection('C'))
  assert.deepEqual(h.selected(), ['C'])
  assertNoDetails(h, 'A')
  assert.equal(h.findRequests('registers', 'C').length, 1)
  assert.equal(h.findRequests('logs', 'C').length, 1)
  await h.showDetails('C', 'C')
  assertDetails(h, 'C')
})

test('a completed log clear for the old connection cannot refresh the newer selection', async (t) => {
  const h = await harness(t)
  await h.selectAndPoll('A')
  await h.showDetails('A', 'A')
  await h.click(h.button('清空'))
  await h.selectAndPoll('B')
  await h.showDetails('B', 'B')
  const requestsBefore = h.findRequests('logs', 'A').length
  await h.settle(h.latest('clear', 'A'), { ok: 'ok' })
  assert.equal(h.findRequests('logs', 'A').length, requestsBefore)
  assertDetails(h, 'B')
})

test('slow responses still update while the following poll is pending', async (t) => {
  const h = await harness(t)
  await h.selectAndPoll('A')
  const completedRegisters = h.latest('registers', 'A')
  const completedLogs = h.latest('logs', 'A')
  await h.tick()
  await h.settle(completedRegisters, [register('completed-register')])
  await h.settle(completedLogs, [log('completed-log')])
  assertDetails(h, 'completed')
  await h.showDetails('A', 'newest')
  assertDetails(h, 'newest')
})
