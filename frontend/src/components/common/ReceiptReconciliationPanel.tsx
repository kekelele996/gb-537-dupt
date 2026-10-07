import { CheckCircleRounded, FactCheckRounded, HistoryToggleOffRounded, PendingActionsRounded, RefreshRounded, RestartAltRounded, SendRounded } from '@mui/icons-material'
import { Alert, Box, Button, Chip, CircularProgress, FormControl, IconButton, InputLabel, MenuItem, Select, Table, TableBody, TableCell, TableHead, TableRow, TextField, Tooltip, Typography } from '@mui/material'
import { useState } from 'react'
import { errorMessage } from '../../api/client'
import { FormDrawer } from '../common/FormDrawer'
import { useAuth } from '../../hooks/useAuth'
import { useDependentServiceStore } from '../../stores/dependent-service'
import { useTrustAnchorStore } from '../../stores/trust-anchor'
import { useRolloverScenarioStore } from '../../stores/rollover-scenario'
import { deliveryLabels, migrationLabels, reconciliationLabels, type MigrationState, type ReconciliationStatus } from '../../types/enums/reconciliation-state'
import type { RolloverScenario, ServiceReconciliation } from '../../types/rollover-scenario'

const statusTone: Record<ReconciliationStatus, string> = {
  reconciled: 'reconciled',
  mismatch_pending_review: 'mismatch',
  outstanding: 'outstanding',
  legacy_missing: 'legacy',
  delivery_failed: 'failed',
}

const statusOrder: ReconciliationStatus[] = ['mismatch_pending_review', 'delivery_failed', 'legacy_missing', 'outstanding', 'reconciled']

export function ReceiptReconciliationPanel({ scenario }: { scenario: RolloverScenario }) {
  const { reconciliation, reconciliationLoading, fetchReceipts, submitReceipt, retryReceipt, reviewReceipt, backfillReceipts } = useRolloverScenarioStore()
  const { items: services } = useDependentServiceStore()
  const { items: anchors } = useTrustAnchorStore()
  const { can, user } = useAuth()
  const [feedback, setFeedback] = useState('')
  const [busyId, setBusyId] = useState<number | null>(null)
  const [reportTarget, setReportTarget] = useState<ServiceReconciliation | null>(null)
  const [reviewTarget, setReviewTarget] = useState<ServiceReconciliation | null>(null)
  const [migration, setMigration] = useState<MigrationState>('in_progress')
  const [trustIds, setTrustIds] = useState<number[]>([])
  const [note, setNote] = useState('')
  const [reviewComment, setReviewComment] = useState('')

  const load = () => { setFeedback(''); void fetchReceipts(scenario.id) }

  const serviceName = (id: number) => services.find((item) => item.id === id)?.name ?? ''
  const ownerTeam = (id: number) => services.find((item) => item.id === id)?.owner_team ?? ''

  const run = async (id: number, action: () => Promise<unknown>) => {
    setBusyId(id); setFeedback('')
    try { await action() } catch (cause) { setFeedback(errorMessage(cause)) } finally { setBusyId(null) }
  }

  const openReport = (item: ServiceReconciliation) => {
    setReportTarget(item)
    setMigration((item.receipt?.migration_state as MigrationState) ?? 'in_progress')
    setTrustIds(item.receipt?.trust_anchor_ids ?? [scenario.new_anchor_id].filter(Boolean))
    setNote(item.receipt?.legacy_backfilled ? '' : item.receipt?.note ?? '')
  }
  const submitReport = async () => {
    if (!reportTarget) return
    await run(reportTarget.service_id, async () => {
      await submitReceipt(scenario.id, { service_id: reportTarget.service_id, migration_state: migration, trust_anchor_ids: trustIds, note })
      setReportTarget(null)
    })
  }

  const canReport = (item: ServiceReconciliation) => {
    if (can('dependency.write') && user?.role === 'pki_operator') return true
    if (user?.role === 'service_owner') return user.team === ownerTeam(item.service_id)
    if (user?.role === 'admin') return true
    return false
  }

  const sorted = [...(reconciliation?.items ?? [])].sort((a, b) => statusOrder.indexOf(a.status) - statusOrder.indexOf(b.status))
  const counts = reconciliation?.counts ?? {}

  return <Box className="receipt-panel">
    <Box className="detail-section-head receipts-head">
      <Typography variant="h3">服务迁移回执对账</Typography>
      <Box className="receipts-actions">
        {can('scenario.write') && <Button size="small" startIcon={<HistoryToggleOffRounded />} disabled={reconciliationLoading} onClick={() => run(0, () => backfillReceipts(scenario.id))}>旧推演补齐回执</Button>}
        <Tooltip title="刷新对账"><span><IconButton size="small" onClick={load} disabled={reconciliationLoading} aria-label="刷新对账">{reconciliationLoading ? <CircularProgress size={16} /> : <RefreshRounded />}</IconButton></span></Tooltip>
      </Box>
    </Box>
    <Alert severity={reconciliation?.ready_to_mark ? 'success' : 'warning'} icon={reconciliation?.ready_to_mark ? <CheckCircleRounded /> : <PendingActionsRounded />}>
      {reconciliation?.ready_to_mark
        ? '每个服务的回执都与冻结推演对平，可以判成就绪。'
        : `还有 ${reconciliation?.blocking.length ?? 0} 个服务对不上：未换完、缺回执、报送失败或挂起待复核时，轮换不能判成就绪。`}
    </Alert>
    <Box className="receipt-counts">
      {statusOrder.map((status) => <Chip key={status} size="small" className={`reconciliation-chip ${statusTone[status]}`} label={`${reconciliationLabels[status]} ${counts[status] ?? 0}`} />)}
    </Box>
    {feedback && <Alert severity="error" onClose={() => setFeedback('')}>{feedback}</Alert>}
    <Table size="small" className="receipt-table">
      <TableHead><TableRow>
        <TableCell>服务</TableCell><TableCell>推演</TableCell><TableCell>回执进度</TableCell><TableCell>报送</TableCell><TableCell>对账</TableCell><TableCell align="right">操作</TableCell>
      </TableRow></TableHead>
      <TableBody>
        {sorted.map((item) => {
          const busy = busyId === item.service_id
          return <TableRow key={item.service_id} className={`receipt-row receipt-row-${statusTone[item.status]}`}>
            <TableCell><strong>{item.service_code}</strong><small>{serviceName(item.service_id)}</small></TableCell>
            <TableCell>{item.predicted_broken ? <Chip size="small" color="error" label="预测会断" /> : <Chip size="small" color="success" variant="outlined" label="不断" />}</TableCell>
            <TableCell>{item.receipt ? <span>{migrationLabels[item.receipt.migration_state]}<small>{item.receipt.reported_by_name}</small></span> : <small>无回执</small>}</TableCell>
            <TableCell>{item.receipt ? <span className={`delivery delivery-${item.receipt.delivery_state}`}>{deliveryLabels[item.receipt.delivery_state]}<small>{item.receipt.delivery_attempts > 1 ? `第 ${item.receipt.delivery_attempts} 次` : ''}</small></span> : '—'}</TableCell>
            <TableCell><Chip size="small" className={`reconciliation-chip ${statusTone[item.status]}`} label={reconciliationLabels[item.status as ReconciliationStatus]} />{item.status === 'mismatch_pending_review' && <small className="receipt-reason">已挂起，待安全复核员</small>}</TableCell>
            <TableCell align="right">
              <Box className="receipt-row-actions">
                {canReport(item) && <Button size="small" variant="outlined" startIcon={<SendRounded />} disabled={busy} onClick={() => openReport(item)}>{item.has_receipt ? '更新回执' : '报送回执'}</Button>}
                {item.status === 'delivery_failed' && canReport(item) && <Button size="small" color="warning" startIcon={<RestartAltRounded />} disabled={busy} onClick={() => run(item.service_id, () => retryReceipt(scenario.id, item.service_id))}>单独重试</Button>}
                {item.status === 'mismatch_pending_review' && can('scenario.verify') && item.receipt && <Button size="small" variant="contained" color="secondary" startIcon={<FactCheckRounded />} disabled={busy || item.receipt.reported_by_name === user?.username} onClick={() => { setReviewTarget(item); setReviewComment('') }}>复核</Button>}
              </Box>
            </TableCell>
          </TableRow>
        })}
        {!sorted.length && <TableRow><TableCell colSpan={6}><Typography className="receipt-empty">{reconciliationLoading ? '正在按服务对账…' : '暂无可对账的服务。'}</Typography></TableCell></TableRow>}
      </TableBody>
    </Table>

    <FormDrawer open={!!reportTarget} onClose={() => setReportTarget(null)} eyebrow="SERVICE MIGRATION RECEIPT" title={`报送回执 · ${reportTarget?.service_code ?? ''}`}>
      <Alert severity="info">由该服务负责人维护本服务的客户端信任集与换到新根的进度。回执只作决策证据，不执行任何部署。</Alert>
      <Box component="form" className="drawer-form" onSubmit={(event) => { event.preventDefault(); void submitReport() }}>
        <FormControl required><InputLabel>迁移进度</InputLabel><Select label="迁移进度" value={migration} onChange={(event) => setMigration(event.target.value as MigrationState)}>
          <MenuItem value="not_started">{migrationLabels.not_started}</MenuItem>
          <MenuItem value="in_progress">{migrationLabels.in_progress}</MenuItem>
          <MenuItem value="migrated">{migrationLabels.migrated}</MenuItem>
        </Select></FormControl>
        <FormControl><InputLabel>客户端信任集（信任锚）</InputLabel><Select multiple label="客户端信任集（信任锚）" value={trustIds} onChange={(event) => setTrustIds(event.target.value as number[])} renderValue={(values) => values.map((id) => anchors.find((anchor) => anchor.id === id)?.anchor_code ?? id).join(', ')}>
          {anchors.map((anchor) => <MenuItem key={anchor.id} value={anchor.id}>{anchor.anchor_code}</MenuItem>)}
        </Select></FormControl>
        <TextField label="备注（报送渠道说明）" value={note} onChange={(event) => setNote(event.target.value)} multiline minRows={2} helperText="填入 SIMULATE_DELIVERY_FAILURE 可模拟该服务回执报送失败并单独重试。" />
        {reportTarget?.predicted_broken && migration === 'migrated' && <Alert severity="warning">推演预测该服务会断裂，却报已换好；提交后将挂起，等待独立安全复核员复核。</Alert>}
        <Button type="submit" variant="contained" startIcon={<SendRounded />} disabled={busyId === reportTarget?.service_id}>提交回执</Button>
      </Box>
    </FormDrawer>

    <FormDrawer open={!!reviewTarget} onClose={() => setReviewTarget(null)} eyebrow="INDEPENDENT SECURITY REVIEW" title={`复核挂起项 · ${reviewTarget?.service_code ?? ''}`}>
      <Alert severity="warning">推演预测该服务信任路径会断，服务侧报已换到新根。请独立核实其客户端信任集确已包含新根；报送人不能复核自己的回执。</Alert>
      <Box className="drawer-form">
        <TextField label="复核意见" value={reviewComment} onChange={(event) => setReviewComment(event.target.value)} multiline minRows={3} placeholder="确认新根指纹已在该服务客户端信任集中。" />
        <Box className="review-decision-row">
          <Button variant="contained" color="success" startIcon={<CheckCircleRounded />} disabled={busyId === reviewTarget?.service_id} onClick={() => reviewTarget && void run(reviewTarget.service_id, async () => { await reviewReceipt(scenario.id, reviewTarget.service_id, 'cleared', reviewComment); setReviewTarget(null) })}>复核通过（解除挂起）</Button>
          <Button variant="outlined" color="error" disabled={busyId === reviewTarget?.service_id} onClick={() => reviewTarget && void run(reviewTarget.service_id, async () => { await reviewReceipt(scenario.id, reviewTarget.service_id, 'rejected', reviewComment); setReviewTarget(null) })}>驳回（保持阻断）</Button>
        </Box>
      </Box>
    </FormDrawer>
  </Box>
}
