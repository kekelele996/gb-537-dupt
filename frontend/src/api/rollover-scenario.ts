import type { Paginated } from '../types/common'
import type { ScenarioState } from '../types/enums/scenario-state'
import type { CreateRolloverScenarioInput, ReceiptReconciliation, MigrationReceipt, RolloverScenario, SubmitMigrationReceiptInput } from '../types/rollover-scenario'
import { apiRequest } from './client'

export const rolloverScenarioApi = {
  list: (query = '') => apiRequest<Paginated<RolloverScenario>>(`/rollover-scenarios${query}`),
  get: (id: number) => apiRequest<RolloverScenario>(`/rollover-scenarios/${id}`),
  create: (input: CreateRolloverScenarioInput) => apiRequest<RolloverScenario>('/rollover-scenarios', { method: 'POST', body: JSON.stringify(input) }),
  simulate: (id: number, idempotencyKey: string) => apiRequest<RolloverScenario>(`/rollover-scenarios/${id}/simulate`, { method: 'POST', headers: { 'Idempotency-Key': idempotencyKey } }),
  transition: (id: number, toState: ScenarioState, comment = '') => apiRequest<RolloverScenario>(`/rollover-scenarios/${id}/transition`, { method: 'POST', body: JSON.stringify({ to_state: toState, comment }) }),
  replay: (id: number) => apiRequest<RolloverScenario>(`/rollover-scenarios/${id}/replay`, { method: 'POST' }),
  compare: (id: number, otherId: number) => apiRequest<Record<string, unknown>>(`/rollover-scenarios/${id}/compare/${otherId}`),
  receipts: (id: number) => apiRequest<ReceiptReconciliation>(`/rollover-scenarios/${id}/receipts`),
  submitReceipt: (id: number, input: SubmitMigrationReceiptInput) => apiRequest<MigrationReceipt>(`/rollover-scenarios/${id}/receipts`, { method: 'POST', body: JSON.stringify(input) }),
  retryReceipt: (id: number, serviceId: number, note = '') => apiRequest<MigrationReceipt>(`/rollover-scenarios/${id}/receipts/${serviceId}/retry`, { method: 'POST', body: JSON.stringify({ note }) }),
  reviewReceipt: (id: number, serviceId: number, decision: 'cleared' | 'rejected', comment: string) => apiRequest<MigrationReceipt>(`/rollover-scenarios/${id}/receipts/${serviceId}/review`, { method: 'POST', body: JSON.stringify({ decision, comment }) }),
  backfillReceipts: (id: number) => apiRequest<ReceiptReconciliation>(`/rollover-scenarios/${id}/receipt-backfill`, { method: 'POST' }),
}

