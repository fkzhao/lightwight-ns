export interface Service {
  id: string
  name: string
  description: string
}

export interface Machine {
  id: string
  name: string
  address: string
  description: string
}

export interface Instance {
  id: string
  serviceId: string
  machineId: string
  port: number
  enabled: boolean
}

export interface RegistrySnapshot {
  version: number
  services: Service[]
  machines: Machine[]
  instances: Instance[]
}
