package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"pki-certificate-rollover-impact/backend/internal/algorithm"
	"pki-certificate-rollover-impact/backend/internal/constants"
	"pki-certificate-rollover-impact/backend/internal/dto"
	"pki-certificate-rollover-impact/backend/internal/model"
	"pki-certificate-rollover-impact/backend/internal/repository"
	"pki-certificate-rollover-impact/backend/internal/util"
)

type ReceiptService struct {
	receipts     repository.ReceiptRepository
	scenarios    repository.RolloverScenarioRepository
	services     repository.DependentServiceRepository
	audits       repository.AuditRepository
	transactions repository.TransactionManager
	now          func() time.Time
}

func NewReceiptService(receipts repository.ReceiptRepository, scenarios repository.RolloverScenarioRepository, services repository.DependentServiceRepository, audits repository.AuditRepository, transactions repository.TransactionManager) *ReceiptService {
	return &ReceiptService{receipts: receipts, scenarios: scenarios, services: services, audits: audits, transactions: transactions, now: func() time.Time { return time.Now().UTC() }}
}

// affectedServiceIDs returns the deduplicated set of service IDs that the
// simulation predicts will break. These are the services whose owners must
// report a switch receipt.
func affectedServiceIDs(scenario model.RolloverScenario) []uint {
	var affected []algorithm.AffectedService
	_ = json.Unmarshal([]byte(scenario.AffectedServicesJSON), &affected)
	seen := map[uint]bool{}
	result := make([]uint, 0, len(affected))
	for _, item := range affected {
		id := item.ServiceID
		if id > 0 && !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

// InitializeForScenario creates a pending receipt for every affected service
// that does not yet have one. Called automatically after a simulation completes
// and by the backfill endpoint for older scenarios.
func (s *ReceiptService) InitializeForScenario(ctx context.Context, scenarioID uint, actor util.Actor, requestID string) (dto.ReceiptListResponse, error) {
	scenario, err := s.scenarios.GetByID(ctx, scenarioID, false)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.ReceiptListResponse{}, util.NotFound("rollover scenario")
		}
		return dto.ReceiptListResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load rollover scenario", err)
	}
	ids := affectedServiceIDs(scenario)
	if len(ids) == 0 {
		return s.List(ctx, scenarioID)
	}
	allServices, err := s.services.All(ctx)
	if err != nil {
		return dto.ReceiptListResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load dependent services", err)
	}
	serviceByID := map[uint]model.DependentService{}
	for _, svc := range allServices {
		serviceByID[svc.ID] = svc
	}
	now := s.now()
	err = s.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		for _, id := range ids {
			if _, existsErr := s.receipts.GetByScenarioAndService(txCtx, scenarioID, id); existsErr == nil {
				continue
			} else if !errors.Is(existsErr, gorm.ErrRecordNotFound) {
				return existsErr
			}
			svc, ok := serviceByID[id]
			if !ok {
				continue
			}
			trustJSON, _ := encode(decodeUintListOrEmpty(svc.ClientTrustRefsJSON))
			receipt := model.Receipt{ScenarioID: scenarioID, ServiceID: id, ServiceCode: svc.ServiceCode, ReportedTrustRefsJSON: trustJSON, SwitchedToNewRoot: false, ReceiptState: string(constants.ReceiptPending), ReportedBy: actor.UserID, ReportedByName: actor.Username, ReportedAt: now, CreatedAt: now, UpdatedAt: now}
			if createErr := s.receipts.Create(txCtx, &receipt); createErr != nil {
				return createErr
			}
		}
		return recordAudit(txCtx, s.audits, actor, requestID, "receipt", scenarioID, "initialize_receipts", nil, map[string]any{"scenario_id": scenarioID, "expected": len(ids)}, "", "", nil, 0, "")
	})
	if err != nil {
		return dto.ReceiptListResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to initialize receipts", err)
	}
	return s.List(ctx, scenarioID)
}

func decodeUintListOrEmpty(raw string) []uint {
	values, err := decodeUintList(raw)
	if err != nil {
		return []uint{}
	}
	return values
}

// Submit records a service owner's switch report for a single service. The
// report is validated against the service's current trust set: claiming a
// switch while the trust set still excludes the new root is a failed
// submission (retryable); claiming a switch when the frozen simulation
// predicted a break is a suspended conflict that needs a reviewer.
func (s *ReceiptService) Submit(ctx context.Context, scenarioID, serviceID uint, request dto.SubmitReceiptRequest, actor util.Actor, requestID string) (dto.ReceiptResponse, error) {
	if err := validateRequest(request); err != nil {
		return dto.ReceiptResponse{}, err
	}
	receipt, err := s.receipts.GetByScenarioAndService(ctx, scenarioID, serviceID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.ReceiptResponse{}, util.NotFound("receipt for this scenario and service")
		}
		return dto.ReceiptResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load receipt", err)
	}
	if receipt.ReceiptState != string(constants.ReceiptPending) && receipt.ReceiptState != string(constants.ReceiptFailed) {
		return dto.ReceiptResponse{}, util.NewError(http.StatusConflict, util.CodeStateTransition, "receipt has already been submitted; use retry for failed receipts")
	}
	service, err := s.services.GetByID(ctx, serviceID, false)
	if err != nil {
		return dto.ReceiptResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load dependent service", err)
	}
	if err := requireReceiptOwnership(actor, service); err != nil {
		return dto.ReceiptResponse{}, err
	}
	scenario, err := s.scenarios.GetByID(ctx, scenarioID, false)
	if err != nil {
		return dto.ReceiptResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load rollover scenario", err)
	}
	currentTrust := decodeUintListOrEmpty(service.ClientTrustRefsJSON)
	trustJSON, _ := encode(currentTrust)
	now := s.now()
	before := receipt
	updates := map[string]any{"reported_trust_refs_json": trustJSON, "switched_to_new_root": request.SwitchedToNewRoot, "reported_by": actor.UserID, "reported_by_name": actor.Username, "reported_at": now, "failure_reason": ""}
	if request.SwitchedToNewRoot {
		if !containsUint(currentTrust, scenario.NewAnchorID) {
			updates["receipt_state"] = string(constants.ReceiptFailed)
			updates["failure_reason"] = "service trust set does not include the new root anchor; update the trust set before reporting a switch"
		} else if containsUint(affectedServiceIDs(scenario), serviceID) {
			updates["receipt_state"] = string(constants.ReceiptSuspended)
		} else {
			updates["receipt_state"] = string(constants.ReceiptSubmitted)
		}
	} else {
		updates["receipt_state"] = string(constants.ReceiptSubmitted)
	}
	err = s.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		if updateErr := s.receipts.Update(txCtx, receipt.ID, updates); updateErr != nil {
			return updateErr
		}
		after := before
		after.ReceiptState = updates["receipt_state"].(string)
		after.SwitchedToNewRoot = request.SwitchedToNewRoot
		after.FailureReason = fmt.Sprint(updates["failure_reason"])
		return recordAudit(txCtx, s.audits, actor, requestID, "receipt", receipt.ID, "submit", before, after, "", "", nil, 0, fmt.Sprint(updates["receipt_state"]))
	})
	if err != nil {
		return dto.ReceiptResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to submit receipt", err)
	}
	return s.getResponse(ctx, receipt.ID)
}

// Retry re-attempts a failed receipt submission against the service's current
// trust set. Only the failed receipt is retried; other services are unaffected.
func (s *ReceiptService) Retry(ctx context.Context, scenarioID, serviceID uint, request dto.SubmitReceiptRequest, actor util.Actor, requestID string) (dto.ReceiptResponse, error) {
	receipt, err := s.receipts.GetByScenarioAndService(ctx, scenarioID, serviceID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.ReceiptResponse{}, util.NotFound("receipt for this scenario and service")
		}
		return dto.ReceiptResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load receipt", err)
	}
	if receipt.ReceiptState != string(constants.ReceiptFailed) {
		return dto.ReceiptResponse{}, util.NewError(http.StatusConflict, util.CodeStateTransition, "only failed receipts can be retried")
	}
	return s.Submit(ctx, scenarioID, serviceID, request, actor, requestID)
}

// Review resolves a suspended receipt. A reviewer confirms the switch (the
// frozen simulation's break prediction is accepted as resolved) or rejects it
// (the service is not actually switched).
func (s *ReceiptService) Review(ctx context.Context, scenarioID, serviceID uint, request dto.ReviewReceiptRequest, actor util.Actor, requestID string) (dto.ReceiptResponse, error) {
	if err := validateRequest(request); err != nil {
		return dto.ReceiptResponse{}, err
	}
	receipt, err := s.receipts.GetByScenarioAndService(ctx, scenarioID, serviceID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.ReceiptResponse{}, util.NotFound("receipt for this scenario and service")
		}
		return dto.ReceiptResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load receipt", err)
	}
	if receipt.ReceiptState != string(constants.ReceiptSuspended) {
		return dto.ReceiptResponse{}, util.NewError(http.StatusConflict, util.CodeStateTransition, "only suspended receipts need review")
	}
	if err := requireReceiptReviewer(actor); err != nil {
		return dto.ReceiptResponse{}, err
	}
	now := s.now()
	state := constants.ReceiptRejected
	if request.Decision == "confirm" {
		state = constants.ReceiptConfirmed
	}
	before := receipt
	updates := map[string]any{"receipt_state": string(state), "reviewed_by": actor.UserID, "reviewed_by_name": actor.Username, "review_comment": strings.TrimSpace(request.Comment), "reviewed_at": now}
	err = s.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		if updateErr := s.receipts.Update(txCtx, receipt.ID, updates); updateErr != nil {
			return updateErr
		}
		after := before
		after.ReceiptState = string(state)
		after.ReviewedBy = &actor.UserID
		after.ReviewedByName = actor.Username
		after.ReviewComment = strings.TrimSpace(request.Comment)
		after.ReviewedAt = &now
		return recordAudit(txCtx, s.audits, actor, requestID, "receipt", receipt.ID, "review", before, after, "", "", nil, 0, request.Decision)
	})
	if err != nil {
		return dto.ReceiptResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to review receipt", err)
	}
	return s.getResponse(ctx, receipt.ID)
}

// Backfill creates pending receipts for affected services that predate the
// receipt feature (older simulated scenarios). Existing receipts are kept.
func (s *ReceiptService) Backfill(ctx context.Context, scenarioID uint, actor util.Actor, requestID string) (dto.ReceiptListResponse, error) {
	return s.InitializeForScenario(ctx, scenarioID, actor, requestID)
}

// CheckReady evaluates the ready gate: a scenario can move to ready only when
// every affected service has an "ok" receipt. Returns the list of blockers
// (empty when ready).
func (s *ReceiptService) CheckReady(ctx context.Context, scenarioID uint) (bool, []string, error) {
	receipts, err := s.receipts.ListByScenario(ctx, scenarioID)
	if err != nil {
		return false, nil, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load receipts", err)
	}
	scenario, err := s.scenarios.GetByID(ctx, scenarioID, false)
	if err != nil {
		return false, nil, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load rollover scenario", err)
	}
	expected := affectedServiceIDs(scenario)
	receiptByService := map[uint]model.Receipt{}
	for _, r := range receipts {
		receiptByService[r.ServiceID] = r
	}
	blockers := []string{}
	for _, id := range expected {
		receipt, ok := receiptByService[id]
		if !ok {
			blockers = append(blockers, fmt.Sprintf("service %d has no receipt", id))
			continue
		}
		status := constants.ReconcileReceipt(constants.ReceiptState(receipt.ReceiptState), receipt.SwitchedToNewRoot)
		switch status {
		case constants.ReconciliationOK:
			continue
		case constants.ReconciliationBlocked:
			blockers = append(blockers, fmt.Sprintf("service %s has not completed the switch", receipt.ServiceCode))
		case constants.ReconciliationSuspended:
			blockers = append(blockers, fmt.Sprintf("service %s receipt is suspended pending security reviewer review", receipt.ServiceCode))
		case constants.ReconciliationFailed:
			blockers = append(blockers, fmt.Sprintf("service %s receipt submission failed and needs retry", receipt.ServiceCode))
		default:
			blockers = append(blockers, fmt.Sprintf("service %s receipt is still pending", receipt.ServiceCode))
		}
	}
	return len(blockers) == 0, blockers, nil
}

// List returns the receipts for a scenario together with the reconciliation
// summary (ready gate, state counts, and mismatches).
func (s *ReceiptService) List(ctx context.Context, scenarioID uint) (dto.ReceiptListResponse, error) {
	scenario, err := s.scenarios.GetByID(ctx, scenarioID, false)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.ReceiptListResponse{}, util.NotFound("rollover scenario")
		}
		return dto.ReceiptListResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load rollover scenario", err)
	}
	receipts, err := s.receipts.ListByScenario(ctx, scenarioID)
	if err != nil {
		return dto.ReceiptListResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to list receipts", err)
	}
	expected := affectedServiceIDs(scenario)
	expectedSet := map[uint]bool{}
	for _, id := range expected {
		expectedSet[id] = true
	}
	items := make([]dto.ReceiptResponse, 0, len(receipts))
	summary := dto.ReconciliationSummary{ScenarioID: scenarioID, TotalExpected: len(expected), Mismatches: []dto.ReconciliationMismatch{}, ReadyBlockers: []string{}}
	receiptByService := map[uint]model.Receipt{}
	for _, r := range receipts {
		receiptByService[r.ServiceID] = r
		items = append(items, dto.NewReceiptResponse(r))
		switch constants.ReceiptState(r.ReceiptState) {
		case constants.ReceiptPending:
			summary.PendingCount++
		case constants.ReceiptFailed:
			summary.FailedCount++
		case constants.ReceiptSubmitted:
			summary.SubmittedCount++
		case constants.ReceiptSuspended:
			summary.SuspendedCount++
		case constants.ReceiptConfirmed:
			summary.ConfirmedCount++
		case constants.ReceiptRejected:
			summary.RejectedCount++
		}
	}
	for _, id := range expected {
		receipt, ok := receiptByService[id]
		if !ok {
			summary.Mismatches = append(summary.Mismatches, dto.ReconciliationMismatch{ServiceID: id, ServiceCode: fmt.Sprint(id), SimulationPrediction: "break predicted", ReceiptStatus: constants.ReconciliationMissing, ReceiptState: "", Detail: "no receipt submitted"})
			continue
		}
		status := constants.ReconcileReceipt(constants.ReceiptState(receipt.ReceiptState), receipt.SwitchedToNewRoot)
		if status != constants.ReconciliationOK {
			summary.Mismatches = append(summary.Mismatches, dto.ReconciliationMismatch{ServiceID: id, ServiceCode: receipt.ServiceCode, SimulationPrediction: "break predicted", ReceiptStatus: status, ReceiptState: receipt.ReceiptState, Detail: mismatchDetail(status)})
		}
	}
	ready, blockers, _ := s.CheckReady(ctx, scenarioID)
	summary.Ready = ready
	summary.ReadyBlockers = blockers
	return dto.ReceiptListResponse{Items: items, Total: int64(len(items)), Page: 1, Size: len(items), Summary: summary}, nil
}

func mismatchDetail(status constants.ReconciliationStatus) string {
	switch status {
	case constants.ReconciliationBlocked:
		return "service reports switch not complete"
	case constants.ReconciliationSuspended:
		return "simulation predicts break but service reports switched; needs reviewer"
	case constants.ReconciliationFailed:
		return "receipt submission failed; retry required"
	case constants.ReconciliationMissing:
		return "no receipt submitted"
	default:
		return ""
	}
}

func (s *ReceiptService) getResponse(ctx context.Context, receiptID uint) (dto.ReceiptResponse, error) {
	receipt, err := s.receipts.GetByID(ctx, receiptID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.ReceiptResponse{}, util.NotFound("receipt")
		}
		return dto.ReceiptResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load receipt", err)
	}
	return dto.NewReceiptResponse(receipt), nil
}

func requireReceiptOwnership(actor util.Actor, service model.DependentService) error {
	if actor.Role == string(constants.RoleAdmin) || actor.Role == string(constants.RolePKIOperator) {
		return nil
	}
	if actor.Role == string(constants.RoleServiceOwner) && actor.Team == service.OwnerTeam {
		return nil
	}
	return util.NewError(http.StatusForbidden, util.CodeForbidden, "service owner may only submit receipts for services owned by their team")
}

func requireReceiptReviewer(actor util.Actor) error {
	if actor.Role == string(constants.RoleAdmin) || actor.Role == string(constants.RoleSecurityReviewer) {
		return nil
	}
	return util.NewError(http.StatusForbidden, util.CodeForbidden, "only a security reviewer may review suspended receipts")
}

func containsUint(values []uint, target uint) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}
