import type { RegistrySnapshot } from "@/types/registry"

export class ApiError extends Error {
  code: string

  constructor(code: string) {
    super(code)
    this.code = code
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let response: Response
  try {
    response = await fetch("/api" + path, {
      ...init,
      headers: { "Content-Type": "application/json", ...init?.headers },
    })
  } catch {
    throw new ApiError("network_error")
  }
  if (!response.ok) {
    const body = await response.json().catch(() => ({})) as { code?: string }
    throw new ApiError(body.code ?? "unknown")
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

export const api = {
  snapshot: () => request<RegistrySnapshot>("/snapshot"),
  save: <T>(kind: "services" | "machines" | "instances", value: T, id?: string) =>
    request<T>("/" + kind + (id ? "/" + id : ""), {
      method: id ? "PUT" : "POST",
      body: JSON.stringify(value),
    }),
  remove: (kind: "services" | "machines" | "instances", id: string) =>
    request<void>("/" + kind + "/" + id, { method: "DELETE" }),
}
