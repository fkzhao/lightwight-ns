import type { HealthCheck, Instance, RegistrySnapshot, Service } from "@/types/registry"

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

export type ServiceInput = Pick<Service, "name" | "description" | "namespace" | "group" | "protection"> & { healthCheck: HealthCheck }
export type InstanceInput = Pick<Instance, "address" | "port" | "enabled"> & Partial<Pick<Instance, "version" | "weight" | "metadata" | "lifecycle">>

export const api = {
  snapshot: () => request<RegistrySnapshot>("/snapshot"),
  saveService: (value: ServiceInput, id?: string) =>
    request<Service>("/services" + (id ? "/" + id : ""), {
      method: id ? "PUT" : "POST",
      body: JSON.stringify(value),
    }),
  removeService: (id: string) => request<void>("/services/" + id, { method: "DELETE" }),
  saveInstance: (serviceId: string, value: InstanceInput, id?: string) =>
    request<Instance>("/services/" + serviceId + "/instances" + (id ? "/" + id : ""), {
      method: id ? "PUT" : "POST",
      body: JSON.stringify(value),
    }),
  setInstanceState: (serviceId: string, id: string, action: "drain" | "enabled", enabled?: boolean) =>
    request<void>("/services/" + serviceId + "/instances/" + id + "/" + action, {
      method: "POST", body: action === "enabled" ? JSON.stringify({ enabled }) : undefined,
    }),
  removeInstance: (serviceId: string, id: string) =>
    request<void>("/services/" + serviceId + "/instances/" + id, { method: "DELETE" }),
}
