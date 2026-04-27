type MockState = {
  storage: Map<string, string>
}

const mockState: MockState = {
  storage: new Map(),
}

const shouldUseMock = () => {
  if (typeof window === 'undefined') {
    return true
  }

  const params = new URLSearchParams(window.location.search)
  return import.meta.env.DEV || params.get('mock') === '1'
}

export class BridgeClient {
  async send<T>(type: string, payload: unknown): Promise<T> {
    if (shouldUseMock()) {
      return mockSend(type, payload) as Promise<T>
    }

    throw new Error(`live bridge boilerplate not implemented for ${type}`)
  }
}

export const bridge = new BridgeClient()

async function mockSend(type: string, payload: unknown): Promise<unknown> {
  switch (type) {
    case 'fs.list':
      return ['Applications', 'Library', 'System', 'Users']
    case 'fs.read':
      return `mock read for ${(payload as { path: string }).path}`
    case 'fs.write':
      return undefined
    case 'storage.get':
      return mockState.storage.get((payload as { key: string }).key) ?? null
    case 'storage.set': {
      const data = payload as { key: string; value: string }
      mockState.storage.set(data.key, data.value)
      return undefined
    }
    case 'storage.delete':
      mockState.storage.delete((payload as { key: string }).key)
      return undefined
    case 'process.launch':
      return { pid: 4242 }
    case 'process.kill':
      return undefined
    default:
      throw new Error(`unknown mock bridge type: ${type}`)
  }
}
