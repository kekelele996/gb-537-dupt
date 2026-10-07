import { create } from 'zustand'
import { rolloverScenarioApi } from '../api/rollover-scenario'
import { errorMessage } from '../api/client'
import type { LoadState } from '../types/common'
import type { CreateRolloverScenarioInput, MigrationReceipt, ReceiptReconciliation, RolloverScenario, SubmitMigrationReceiptInput } from '../types/rollover-scenario'
import type { ScenarioState } from '../types/enums/scenario-state'

interface RolloverScenarioState {
  items: RolloverScenario[]
  total: number
  status: LoadState
  error: string
  active: RolloverScenario | null
  reconciliation: ReceiptReconciliation | null
  reconciliationLoading: boolean
  fetchScenarios: (query?: string) => Promise<void>
  createScenario: (input: CreateRolloverScenarioInput) => Promise<RolloverScenario>
  simulate: (id: number, key: string) => Promise<RolloverScenario>
  transition: (id: number, state: ScenarioState, comment?: string) => Promise<RolloverScenario>
  replay: (id: number) => Promise<RolloverScenario>
  fetchReceipts: (id: number) => Promise<void>
  submitReceipt: (id: number, input: SubmitMigrationReceiptInput) => Promise<MigrationReceipt>
  retryReceipt: (id: number, serviceId: number, note?: string) => Promise<MigrationReceipt>
  reviewReceipt: (id: number, serviceId: number, decision: 'cleared' | 'rejected', comment: string) => Promise<MigrationReceipt>
  backfillReceipts: (id: number) => Promise<void>
  select: (scenario: RolloverScenario | null) => void
}

export const useRolloverScenarioStore = create<RolloverScenarioState>((set, get) => {
  const merge = (updated: RolloverScenario) => set({
    active: updated,
    items: get().items.map((item) => item.id === updated.id ? updated : item),
  })
  return {
    items: [], total: 0, status: 'idle', error: '', active: null,
    reconciliation: null, reconciliationLoading: false,
    fetchScenarios: async (query = '') => {
      set({ status: 'loading', error: '' })
      try {
        const result = await rolloverScenarioApi.list(query)
        set({ items: result.items, total: result.total, status: 'ready' })
      } catch (error) { set({ status: 'error', error: errorMessage(error) }) }
    },
    createScenario: async (input) => {
      const created = await rolloverScenarioApi.create(input)
      set({ items: [created, ...get().items], total: get().total + 1, active: created })
      return created
    },
    simulate: async (id, key) => { const updated = await rolloverScenarioApi.simulate(id, key); merge(updated); return updated },
    transition: async (id, state, comment) => { const updated = await rolloverScenarioApi.transition(id, state, comment); merge(updated); return updated },
    replay: async (id) => { const updated = await rolloverScenarioApi.replay(id); merge(updated); return updated },
    fetchReceipts: async (id) => {
      set({ reconciliationLoading: true })
      try {
        const reconciliation = await rolloverScenarioApi.receipts(id)
        set({ reconciliation, reconciliationLoading: false })
      } catch (error) {
        set({ reconciliationLoading: false, error: errorMessage(error) })
      }
    },
    submitReceipt: async (id, input) => {
      const receipt = await rolloverScenarioApi.submitReceipt(id, input)
      await get().fetchReceipts(id)
      return receipt
    },
    retryReceipt: async (id, serviceId, note) => {
      const receipt = await rolloverScenarioApi.retryReceipt(id, serviceId, note)
      await get().fetchReceipts(id)
      return receipt
    },
    reviewReceipt: async (id, serviceId, decision, comment) => {
      const receipt = await rolloverScenarioApi.reviewReceipt(id, serviceId, decision, comment)
      await get().fetchReceipts(id)
      return receipt
    },
    backfillReceipts: async (id) => { await rolloverScenarioApi.backfillReceipts(id); await get().fetchReceipts(id) },
    select: (scenario) => set({ active: scenario, reconciliation: null }),
  }
})
