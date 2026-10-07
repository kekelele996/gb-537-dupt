import type { Receipt, ReceiptListResponse, ReviewReceiptInput, SubmitReceiptInput } from '../types/receipt'
import { apiRequest } from './client'

export const receiptApi = {
  list: (scenarioId: number) => apiRequest<ReceiptListResponse>(`/rollover-scenarios/${scenarioId}/receipts`),
  backfill: (scenarioId: number) => apiRequest<ReceiptListResponse>(`/rollover-scenarios/${scenarioId}/receipts/backfill`, { method: 'POST' }),
  submit: (scenarioId: number, serviceId: number, input: SubmitReceiptInput) => apiRequest<Receipt>(`/rollover-scenarios/${scenarioId}/receipts/${serviceId}/submit`, { method: 'POST', body: JSON.stringify(input) }),
  retry: (scenarioId: number, serviceId: number, input: SubmitReceiptInput) => apiRequest<Receipt>(`/rollover-scenarios/${scenarioId}/receipts/${serviceId}/retry`, { method: 'POST', body: JSON.stringify(input) }),
  review: (scenarioId: number, serviceId: number, input: ReviewReceiptInput) => apiRequest<Receipt>(`/rollover-scenarios/${scenarioId}/receipts/${serviceId}/review`, { method: 'POST', body: JSON.stringify(input) }),
}
