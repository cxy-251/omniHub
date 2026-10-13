export type User = { id: number; username: string; role: 'admin' | 'user'; level: number };
export type Status = { logged_in: boolean; has_admin: boolean; user: User };

// request is shared by typed components and the transitional module adapters.
export async function request(path: string, options: RequestInit = {}): Promise<Response> {
  const response = await fetch(path, { ...options, credentials: 'same-origin' });
  if (!response.ok) {
    const raw = await response.text();
    let message = raw;
    try { message = (JSON.parse(raw) as { error?: string }).error ?? raw; } catch { /* plain HTTP error */ }
    throw new Error(message.trim() || `请求失败 (${response.status})`);
  }
  return response;
}

export async function api<T>(path: string, options: RequestInit = {}): Promise<T> {
  return (await request(path, options)).json() as Promise<T>;
}

export function post<T>(path: string, data: unknown): Promise<T> {
  return api<T>(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data) });
}
