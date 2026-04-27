import { bridge } from './bridge'

export const fs = {
  read: (path: string) => bridge.send<string>('fs.read', { path }),
  write: (path: string, data: string) => bridge.send<void>('fs.write', { path, data }),
  list: (path: string) => bridge.send<string[]>('fs.list', { path }),
}

