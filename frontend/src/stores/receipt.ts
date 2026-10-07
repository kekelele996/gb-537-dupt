import { create } from 'zustand'
import { receiptApi } from '../api/receipt'
import { errorMessage } from '../api/client'
import type { Receipt, ReceiptListResponse, ReviewReceiptInput, SubmitReceiptInput } from '../types/receipt'

interface ReceiptState {
  receipts: Receipt[]
  summary: ReceiptListResponse['summary'] | null
  status: 'idle' | 'loading' | 'ready' | 'error'
  error: string
  activeScenarioId: number | null
  fetchReceipts: (scenarioId: number) => Promise<void>
  backfill: (scenarioId: number) => Promise<void>
  submit: (scenarioId: number, serviceId: number, input: SubmitReceiptInput) => Promise<Receipt>
  retry: (scenarioId: number, serviceId: number, input: SubmitReceiptInput) => Promise<Receipt>
  review: (scenarioId: number, serviceId: number, input: ReviewReceiptInput) => Promise<Receipt>
}

const emptySummary: ReceiptListResponse['summary'] = {
  scenario_id: 0, total_expected: 0, pending_count: 0, failed_count: 0, submitted_count: 0,
  suspended_count: 0, confirmed_count: 0, rejected_count: 0, ready: false, ready_blockers: [], mismatches: [],
}

export const useReceiptStore = create<ReceiptState>((set, get) => {
  const applyList = (result: ReceiptListResponse) => set({
    receipts: result.items,
    summary: result.summary,
    status: 'ready',
    activeScenarioId: result.summary.scenario_id,
  })
  const mergeReceipt = (updated: Receipt) => set({
    receipts: get().receipts.map((r) => r.id === updated.id ? updated : r),
  })
  return {
    receipts: [], summary: null, status: 'idle', error: '', activeScenarioId: null,
    fetchReceipts: async (scenarioId) => {
      set({ status: 'loading', error: '' })
      try {
        const result = await receiptApi.list(scenarioId)
        applyList(result)
      } catch (error) { set({ status: 'error', error: errorMessage(error) }) }
    },
    backfill: async (scenarioId) => {
      const result = await receiptApi.backfill(scenarioId)
      applyList(result)
    },
    submit: async (scenarioId, serviceId, input) => {
      const updated = await receiptApi.submit(scenarioId, serviceId, input)
      mergeReceipt(updated)
      return updated
    },
    retry: async (scenarioId, serviceId, input) => {
      const updated = await receiptApi.retry(scenarioId, serviceId, input)
      mergeReceipt(updated)
      return updated
    },
    review: async (scenarioId, serviceId, input) => {
      const updated = await receiptApi.review(scenarioId, serviceId, input)
      mergeReceipt(updated)
      return updated
    },
  }
})

export { emptySummary }
