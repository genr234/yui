import { bridge } from './bridge'

export const fs = {
  read: (path: string) => bridge.send<string>('fs.read', { path }),
  list: (path: string) => bridge.send<string[]>('fs.list', { path }),
}
