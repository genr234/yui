type Message = {
  id: string;
  method: string;
  params?: any;
};

type Response = {
  id: string;
  result?: any;
  error?: string;
};

export class Bridge {
  private socket: WebSocket;
  private pending: Map<string, (response: Response) => void> = new Map();

  constructor() {
    this.socket = new WebSocket("localhost:7071");
    this.socket.onmessage = (event) => {
      const response: Response = JSON.parse(event.data);
      const callback = this.pending.get(response.id);
      if (callback) {
        callback(response);
        this.pending.delete(response.id);
      }
    };
  }

  send(method: string, payload: Record<string, unknown> = {}): Promise<Response> {
    return new Promise((resolve) => {
      const id = crypto.randomUUID();
      const message: Message = { id, method, params: payload };
      this.socket.send(JSON.stringify(message));
      this.pending.set(id, resolve);
    });
  }
}