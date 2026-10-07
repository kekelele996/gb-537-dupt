export const reconciliationStates = [
  'reconciled',
  'mismatch_pending_review',
  'outstanding',
  'legacy_missing',
  'delivery_failed',
] as const
export type ReconciliationStatus = (typeof reconciliationStates)[number]

export type MigrationState = 'not_started' | 'in_progress' | 'migrated'
export type DeliveryState = 'pending' | 'delivered' | 'failed'
export type ReviewStatus = 'not_required' | 'pending' | 'cleared' | 'rejected'

export const reconciliationLabels: Record<ReconciliationStatus, string> = {
  reconciled: '已对平',
  mismatch_pending_review: '挂起待复核',
  outstanding: '未换完',
  legacy_missing: '旧推演缺回执',
  delivery_failed: '报送失败',
}

export const migrationLabels: Record<MigrationState, string> = {
  not_started: '未开始',
  in_progress: '切换中',
  migrated: '已换到新根',
}

export const deliveryLabels: Record<DeliveryState, string> = {
  pending: '待报送',
  delivered: '已送达',
  failed: '报送失败',
}
