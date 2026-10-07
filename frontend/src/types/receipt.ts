export type ReceiptState = 'pending' | 'failed' | 'submitted' | 'suspended' | 'confirmed' | 'rejected'
export type ReconciliationStatus = 'ok' | 'blocked' | 'suspended' | 'missing' | 'failed'

export interface Receipt {
  id: number
  scenario_id: number
  service_id: number
  service_code: string
  reported_trust_refs_json: number[]
  switched_to_new_root: boolean
  receipt_state: ReceiptState
  reconciliation_status: ReconciliationStatus
  failure_reason: string
  reported_by: number
  reported_by_name: string
  reported_at: string
  reviewed_by?: number
  reviewed_by_name: string
  review_comment: string
  reviewed_at?: string
  created_at: string
  updated_at: string
}

export interface ReconciliationMismatch {
  service_id: number
  service_code: string
  simulation_prediction: string
  receipt_status: ReconciliationStatus
  receipt_state: string
  detail: string
}

export interface ReconciliationSummary {
  scenario_id: number
  total_expected: number
  pending_count: number
  failed_count: number
  submitted_count: number
  suspended_count: number
  confirmed_count: number
  rejected_count: number
  ready: boolean
  ready_blockers: string[]
  mismatches: ReconciliationMismatch[]
}

export interface ReceiptListResponse {
  items: Receipt[]
  total: number
  page: number
  size: number
  summary: ReconciliationSummary
}

export interface SubmitReceiptInput {
  switched_to_new_root: boolean
  comment?: string
}

export interface ReviewReceiptInput {
  decision: 'confirm' | 'reject'
  comment?: string
}
