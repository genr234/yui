import { bridge } from './bridge'

export const process = {
  launch: (exe: string, args: string[]) => bridge.send<{ pid: number }>('process.launch', { exe, args }),
  kill: (pid: number) => bridge.send<void>('process.kill', { pid }),
}
