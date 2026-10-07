import type { ScenarioState } from './enums/scenario-state'
import type { MigrationState, DeliveryState, ReviewStatus, ReconciliationStatus } from './enums/reconciliation-state'
import type { TrustAnchor } from './trust-anchor'

export interface AffectedService {
  id: number
  code: string
  state: string
  not_before?: string
  not_after?: string
  service_id?: number
  service_code?: string
  criticality?: string
  at?: string
  reason?: string
}

export interface BrokenPath {
  at: string
  service_codes: string[]
  reason: string
}

export interface ServiceEvidence {
  service_id: number
  service_code: string
  reachable: boolean
  selected_chain_id?: number
  selected_anchor_id?: number
  reason: string
}

export interface TimepointEvidence {
  at: string
  active_anchor_ids: number[]
  services: ServiceEvidence[]
}

export interface RolloverScenario {
  id: number
  name: string
  old_anchor_id: number
  new_anchor_id: number
  old_anchor?: TrustAnchor
  new_anchor?: TrustAnchor
  overlap_start: string
  overlap_end: string
  candidate_chain_ids: number[]
  algorithm_version: string
  input_hash: string
  simulation_time: string
  affected_services_json: AffectedService[]
  broken_paths_json: BrokenPath[]
  path_evidence_json: TimepointEvidence[]
  scenario_state: ScenarioState
  explanation: string
  created_by: number
  created_by_name: string
  verified_by?: number
  verified_by_name: string
  replay_verified: boolean
  duration_ms: number
  rollback_record: string
  created_at: string
  updated_at: string
}

export interface CreateRolloverScenarioInput {
  name: string
  old_anchor_id: number
  new_anchor_id: number
  overlap_start: string
  overlap_end: string
  candidate_chain_ids: number[]
  simulation_time: string
}

export interface MigrationReceipt {
  id: number
  scenario_id: number
  service_id: number
  service_code: string
  migration_state: MigrationState
  trust_anchor_ids: number[]
  note: string
  delivery_state: DeliveryState
  delivery_attempts: number
  last_delivery_error: string
  review_status: ReviewStatus
  reviewed_by_name: string
  reviewed_at?: string
  review_comment: string
  legacy_backfilled: boolean
  reported_by_name: string
  delivered_at?: string
  created_at: string
  updated_at: string
}

export interface SubmitMigrationReceiptInput {
  service_id: number
  migration_state: MigrationState
  trust_anchor_ids: number[]
  note?: string
}

export interface ServiceReconciliation {
  service_id: number
  service_code: string
  predicted_broken: boolean
  has_receipt: boolean
  receipt?: MigrationReceipt
  status: ReconciliationStatus
  reason: string
}

export interface ReceiptReconciliation {
  scenario_id: number
  scenario_state: ScenarioState
  total_services: number
  counts: Partial<Record<ReconciliationStatus, number>>
  ready_to_mark: boolean
  blocking: ServiceReconciliation[]
  items: ServiceReconciliation[]
}

