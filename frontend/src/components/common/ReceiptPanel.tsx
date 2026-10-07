import { CheckCircleRounded, ErrorRounded, GppMaybeRounded, HourglassEmptyRounded, RefreshRounded, VerifiedUserRounded } from '@mui/icons-material'
import { Alert, Box, Button, Chip, IconButton, Tooltip, Typography } from '@mui/material'
import { useEffect, useState } from 'react'
import { errorMessage } from '../../api/client'
import { useAuth } from '../../hooks/useAuth'
import { useReceiptStore } from '../../stores/receipt'
import type { Receipt, ReceiptState, ReconciliationStatus, ReconciliationMismatch } from '../../types/receipt'
import { formatDateTime } from '../../utils/date'

const receiptStateLabels: Record<ReceiptState, string> = {
  pending: '待报送', failed: '报送失败', submitted: '已报送', suspended: '待复核', confirmed: '已确认', rejected: '已驳回',
}

const reconciliationLabels: Record<ReconciliationStatus, string> = {
  ok: '一致', blocked: '未完成', suspended: '待复核', missing: '缺回执', failed: '报送失败',
}

function ReceiptStateBadge({ state }: { state: ReceiptState }) {
  return <Box component="span" className={`state-badge receipt-${state}`}><span className="state-dot" />{receiptStateLabels[state]}</Box>
}

function ReconciliationBadge({ status }: { status: ReconciliationStatus }) {
  return <Box component="span" className={`tone-badge reconciliation-${status}`}>{reconciliationLabels[status]}</Box>
}

export function ReceiptPanel({ scenarioId }: { scenarioId: number }) {
  const { receipts, summary, status, error, fetchReceipts, backfill, submit, retry, review } = useReceiptStore()
  const { can } = useAuth()
  const [feedback, setFeedback] = useState('')
  const [busyService, setBusyService] = useState<number | null>(null)

  useEffect(() => { void fetchReceipts(scenarioId) }, [scenarioId, fetchReceipts])

  const canSubmit = can('receipt.submit')
  const canReview = can('receipt.review')
  const canBackfill = can('scenario.write')

  const handleBackfill = async () => {
    setFeedback('')
    try { await backfill(scenarioId) } catch (cause) { setFeedback(errorMessage(cause)) }
  }

  const handleSubmit = async (serviceId: number, switched: boolean) => {
    setBusyService(serviceId); setFeedback('')
    try { await submit(scenarioId, serviceId, { switched_to_new_root: switched }) } catch (cause) { setFeedback(errorMessage(cause)) } finally { setBusyService(null) }
  }

  const handleRetry = async (serviceId: number, switched: boolean) => {
    setBusyService(serviceId); setFeedback('')
    try { await retry(scenarioId, serviceId, { switched_to_new_root: switched }) } catch (cause) { setFeedback(errorMessage(cause)) } finally { setBusyService(null) }
  }

  const handleReview = async (serviceId: number, decision: 'confirm' | 'reject') => {
    setBusyService(serviceId); setFeedback('')
    try { await review(scenarioId, serviceId, { decision }) } catch (cause) { setFeedback(errorMessage(cause)) } finally { setBusyService(null) }
  }

  if (status === 'loading' && !receipts.length) return <Box className="receipt-panel-loading"><Typography>加载回执中…</Typography></Box>
  if (status === 'error' && !receipts.length) return <Alert severity="error">{error}</Alert>

  const hasReceipts = receipts.length > 0
  const ready = summary?.ready ?? false

  return <Box className="receipt-panel">
    <Box className="detail-section-head">
      <Box><Typography variant="h3">回执对账</Typography><span>服务负责人报送换根进度，与推演预测按服务对账</span></Box>
      <Box className="receipt-panel-actions">
        <Tooltip title="刷新回执"><IconButton onClick={() => fetchReceipts(scenarioId)} aria-label="刷新回执"><RefreshRounded /></IconButton></Tooltip>
        {canBackfill && <Button size="small" variant="outlined" onClick={handleBackfill}>补齐回执</Button>}
      </Box>
    </Box>

    {feedback && <Alert severity="error" onClose={() => setFeedback('')}>{feedback}</Alert>}

    {!hasReceipts && <Alert severity="info">当前推演还没有回执。点击「补齐回执」为所有受影响服务创建待报送回执。</Alert>}

    {hasReceipts && <>
      <Box className="receipt-summary">
        <Chip icon={ready ? <CheckCircleRounded /> : <ErrorRounded />} label={ready ? '就绪条件已满足' : '轮换未就绪'} color={ready ? 'success' : 'warning'} variant="outlined" />
        <Box className="receipt-counts">
          <span className="receipt-count"><HourglassEmptyRounded />待报送 {summary?.pending_count ?? 0}</span>
          <span className="receipt-count"><ErrorRounded />报送失败 {summary?.failed_count ?? 0}</span>
          <span className="receipt-count"><CheckCircleRounded />已报送 {summary?.submitted_count ?? 0}</span>
          <span className="receipt-count"><GppMaybeRounded />待复核 {summary?.suspended_count ?? 0}</span>
          <span className="receipt-count"><VerifiedUserRounded />已确认 {summary?.confirmed_count ?? 0}</span>
        </Box>
      </Box>

      {!ready && summary && summary.ready_blockers.length > 0 && <Alert severity="warning" className="receipt-blockers"><Typography component="strong">就绪阻塞项：</Typography><ul>{summary.ready_blockers.map((blocker: string, i: number) => <li key={i}>{blocker}</li>)}</ul></Alert>}

      <Box className="receipt-list">
        {receipts.map((receipt: Receipt) => <Box key={receipt.id} className="receipt-row">
          <Box className="receipt-row-main">
            <Box className="receipt-row-title">
              <Typography component="strong">{receipt.service_code}</Typography>
              <ReceiptStateBadge state={receipt.receipt_state} />
              <ReconciliationBadge status={receipt.reconciliation_status} />
            </Box>
            <Box className="receipt-row-meta">
              <span>报送人：{receipt.reported_by_name || '—'}</span>
              <span>报送时间：{formatDateTime(receipt.reported_at)}</span>
              {receipt.reviewed_by_name && <span>复核人：{receipt.reviewed_by_name}</span>}
              {receipt.reviewed_at && <span>复核时间：{formatDateTime(receipt.reviewed_at)}</span>}
            </Box>
            {receipt.failure_reason && <Typography className="receipt-failure">{receipt.failure_reason}</Typography>}
            {receipt.review_comment && <Typography className="receipt-review-comment">复核意见：{receipt.review_comment}</Typography>}
          </Box>
          <Box className="receipt-row-actions">
            {receipt.receipt_state === 'pending' && canSubmit && <>
              <Button size="small" variant="contained" disabled={busyService === receipt.service_id} onClick={() => handleSubmit(receipt.service_id, true)}>已换新根</Button>
              <Button size="small" variant="outlined" disabled={busyService === receipt.service_id} onClick={() => handleSubmit(receipt.service_id, false)}>尚未换完</Button>
            </>}
            {receipt.receipt_state === 'failed' && canSubmit && <>
              <Button size="small" variant="contained" disabled={busyService === receipt.service_id} onClick={() => handleRetry(receipt.service_id, true)}>重试报送</Button>
            </>}
            {receipt.receipt_state === 'suspended' && canReview && <>
              <Button size="small" variant="contained" color="success" disabled={busyService === receipt.service_id} onClick={() => handleReview(receipt.service_id, 'confirm')}>确认换好</Button>
              <Button size="small" variant="outlined" color="error" disabled={busyService === receipt.service_id} onClick={() => handleReview(receipt.service_id, 'reject')}>驳回</Button>
            </>}
          </Box>
        </Box>)}
      </Box>

      {summary && summary.mismatches.length > 0 && <Box className="receipt-mismatches">
        <Typography variant="h4">对账差异</Typography>
        <Box className="mismatch-list">
          {summary.mismatches.map((mismatch: ReconciliationMismatch) => <Box key={mismatch.service_id} className="mismatch-row">
            <Typography component="strong">{mismatch.service_code}</Typography>
            <span>推演预测：{mismatch.simulation_prediction}</span>
            <span>回执状态：{reconciliationLabels[mismatch.receipt_status]}</span>
            <Typography className="mismatch-detail">{mismatch.detail}</Typography>
          </Box>)}
        </Box>
      </Box>}
    </>}
  </Box>
}
