package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"net/http"
	"pki-certificate-rollover-impact/backend/internal/algorithm"
	"pki-certificate-rollover-impact/backend/internal/constants"
	"pki-certificate-rollover-impact/backend/internal/dto"
	"pki-certificate-rollover-impact/backend/internal/model"
	"pki-certificate-rollover-impact/backend/internal/repository"
	"pki-certificate-rollover-impact/backend/internal/util"
	"strings"
	"time"
)

type RolloverScenarioService struct {
	scenarios    repository.RolloverScenarioRepository
	anchors      repository.TrustAnchorRepository
	chains       repository.CertificateChainRepository
	services     repository.DependentServiceRepository
	audits       repository.AuditRepository
	transactions repository.TransactionManager
	now          func() time.Time
}

func NewRolloverScenarioService(scenarios repository.RolloverScenarioRepository, anchors repository.TrustAnchorRepository, chains repository.CertificateChainRepository, services repository.DependentServiceRepository, audits repository.AuditRepository, transactions repository.TransactionManager) *RolloverScenarioService {
	return &RolloverScenarioService{scenarios: scenarios, anchors: anchors, chains: chains, services: services, audits: audits, transactions: transactions, now: func() time.Time { return time.Now().UTC() }}
}

func requireScenarioOwnership(actor util.Actor, scenario model.RolloverScenario) error {
	if actor.Role == string(constants.RoleAdmin) || actor.Role == string(constants.RolePKIOperator) || scenario.CreatedBy == actor.UserID {
		return nil
	}
	return util.NewError(http.StatusForbidden, util.CodeForbidden, "only the scenario creator or a PKI administrator may manage this scenario")
}
func (s *RolloverScenarioService) Create(ctx context.Context, request dto.CreateRolloverScenarioRequest, actor util.Actor, requestID string) (dto.RolloverScenarioResponse, error) {
	if err := validateRequest(request); err != nil {
		return dto.RolloverScenarioResponse{}, err
	}
	if request.OldAnchorID == request.NewAnchorID {
		return dto.RolloverScenarioResponse{}, util.NewError(http.StatusBadRequest, util.CodeValidation, "old and new trust anchors must be distinct")
	}
	anchorIDs := uniqueIDs([]uint{request.OldAnchorID, request.NewAnchorID})
	anchors, err := s.anchors.GetByIDs(ctx, anchorIDs)
	if err != nil || len(anchors) != 2 {
		return dto.RolloverScenarioResponse{}, util.NewError(http.StatusBadRequest, util.CodeValidation, "both trust anchors must exist")
	}
	candidateIDs := uniqueIDs(request.CandidateChainIDs)
	candidates, err := s.chains.GetByIDs(ctx, candidateIDs)
	if err != nil || len(candidates) != len(candidateIDs) {
		return dto.RolloverScenarioResponse{}, util.NewError(http.StatusBadRequest, util.CodeValidation, "one or more candidate chains do not exist")
	}
	allChains, _, err := s.chains.List(ctx, dto.CertificateChainQuery{Page: 1, PageSize: 200})
	if err != nil {
		return dto.RolloverScenarioResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to freeze certificate chains", err)
	}
	allServices, err := s.services.All(ctx)
	if err != nil {
		return dto.RolloverScenarioResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to freeze dependency graph", err)
	}
	snapshot, err := buildSnapshot(request.Name, request.OldAnchorID, request.NewAnchorID, request.OverlapStart, request.OverlapEnd, request.SimulationTime, candidateIDs, anchors, allChains, allServices)
	if err != nil {
		return dto.RolloverScenarioResponse{}, util.WrapError(http.StatusUnprocessableEntity, util.CodeValidation, "rollover input is invalid", err)
	}
	inputHash, err := snapshot.Hash()
	if err != nil {
		return dto.RolloverScenarioResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to hash frozen input", err)
	}
	snapshotJSON, _ := snapshot.Canonical()
	candidateJSON, _ := encode(candidateIDs)
	now := s.now()
	scenario := model.RolloverScenario{Name: strings.TrimSpace(request.Name), OldAnchorID: request.OldAnchorID, NewAnchorID: request.NewAnchorID, OverlapStart: request.OverlapStart.UTC(), OverlapEnd: request.OverlapEnd.UTC(), CandidateChainIDs: candidateJSON, AlgorithmVersion: algorithm.Version, InputHash: inputHash, InputSnapshot: snapshotJSON, SimulationTime: request.SimulationTime.UTC(), AffectedServicesJSON: "[]", BrokenPathsJSON: "[]", PathEvidenceJSON: "[]", ScenarioState: string(constants.ScenarioDraft), Explanation: "Frozen input is ready for offline simulation.", CreatedBy: actor.UserID, CreatedByName: actor.Username, CreatedAt: now, UpdatedAt: now}
	err = s.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		if createErr := s.scenarios.Create(txCtx, &scenario); createErr != nil {
			return createErr
		}
		return recordAudit(txCtx, s.audits, actor, requestID, "rollover_scenario", scenario.ID, "create_frozen_draft", nil, scenario, inputHash, algorithm.Version, &scenario.SimulationTime, 0, "frozen anchors, chains, and dependency graph")
	})
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return dto.RolloverScenarioResponse{}, util.WrapError(http.StatusConflict, util.CodeConflict, "an identical frozen scenario already exists", err)
		}
		return dto.RolloverScenarioResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to create rollover scenario", err)
	}
	return s.Get(ctx, scenario.ID)
}
func (s *RolloverScenarioService) Simulate(ctx context.Context, id uint, idempotencyKey string, actor util.Actor, requestID string) (dto.RolloverScenarioResponse, bool, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return dto.RolloverScenarioResponse{}, false, util.NewError(http.StatusBadRequest, util.CodeIdempotency, "Idempotency-Key header is required")
	}
	if len(idempotencyKey) > 128 {
		return dto.RolloverScenarioResponse{}, false, util.NewError(http.StatusBadRequest, util.CodeValidation, "Idempotency-Key must not exceed 128 characters")
	}
	scenario, err := s.scenarios.GetByID(ctx, id, false)
	if err != nil {
		return dto.RolloverScenarioResponse{}, false, util.NotFound("rollover scenario")
	}
	if err := requireScenarioOwnership(actor, scenario); err != nil {
		return dto.RolloverScenarioResponse{}, false, err
	}
	if prior, err := s.scenarios.FindByIdempotencyKey(ctx, idempotencyKey); err == nil {
		if prior.ID != id {
			return dto.RolloverScenarioResponse{}, false, util.NewError(http.StatusConflict, util.CodeIdempotency, "Idempotency-Key is already bound to another rollover scenario")
		}
		return dto.NewRolloverScenarioResponse(prior, s.now()), true, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return dto.RolloverScenarioResponse{}, false, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to check idempotency key", err)
	}
	if scenario.ScenarioState != "draft" {
		return dto.RolloverScenarioResponse{}, false, util.NewError(http.StatusConflict, util.CodeStateTransition, "only draft scenarios can be simulated")
	}
	snapshot, err := algorithm.DecodeSnapshot(scenario.InputSnapshot)
	if err != nil {
		return dto.RolloverScenarioResponse{}, false, util.WrapError(http.StatusUnprocessableEntity, util.CodeValidation, "frozen scenario snapshot is invalid", err)
	}
	started := time.Now()
	result, err := algorithm.Simulate(snapshot)
	duration := time.Since(started).Milliseconds()
	if err != nil {
		return dto.RolloverScenarioResponse{}, false, util.WrapError(http.StatusUnprocessableEntity, util.CodeValidation, "rollover simulation failed", err)
	}
	affectedJSON, _ := encode(result.AffectedServices)
	pathsJSON, _ := encode(result.BrokenPaths)
	evidenceJSON, _ := encode(result.Evidence)
	before := scenario
	updates := map[string]any{"affected_services_json": affectedJSON, "broken_paths_json": pathsJSON, "path_evidence_json": evidenceJSON, "explanation": result.Explanation, "duration_ms": duration, "idempotency_key": idempotencyKey}
	err = s.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		changed, completeErr := s.scenarios.CompleteSimulation(txCtx, id, updates)
		if completeErr != nil {
			return completeErr
		}
		if !changed {
			return util.NewError(http.StatusConflict, util.CodeConflict, "scenario state changed concurrently")
		}
		scenario.ScenarioState = "simulated"
		scenario.AffectedServicesJSON = affectedJSON
		scenario.BrokenPathsJSON = pathsJSON
		scenario.PathEvidenceJSON = evidenceJSON
		scenario.Explanation = result.Explanation
		scenario.DurationMS = duration
		scenario.IdempotencyKey = idempotencyKey
		return recordAudit(txCtx, s.audits, actor, requestID, "rollover_scenario", id, "simulate", before, scenario, scenario.InputHash, scenario.AlgorithmVersion, &scenario.SimulationTime, duration, result.Explanation)
	})
	if err != nil {
		return dto.RolloverScenarioResponse{}, false, err
	}
	response, getErr := s.Get(ctx, id)
	return response, false, getErr
}
func (s *RolloverScenarioService) Get(ctx context.Context, id uint) (dto.RolloverScenarioResponse, error) {
	scenario, err := s.scenarios.GetByID(ctx, id, true)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.RolloverScenarioResponse{}, util.NotFound("rollover scenario")
		}
		return dto.RolloverScenarioResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load rollover scenario", err)
	}
	return dto.NewRolloverScenarioResponse(scenario, s.now()), nil
}
func (s *RolloverScenarioService) List(ctx context.Context, query dto.RolloverScenarioQuery) (dto.RolloverScenarioListResponse, error) {
	scenarios, total, err := s.scenarios.List(ctx, query)
	if err != nil {
		return dto.RolloverScenarioListResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to list rollover scenarios", err)
	}
	response := dto.RolloverScenarioListResponse{Items: make([]dto.RolloverScenarioResponse, 0, len(scenarios)), Total: total, Page: query.Page, Size: query.PageSize}
	for _, scenario := range scenarios {
		response.Items = append(response.Items, dto.NewRolloverScenarioResponse(scenario, s.now()))
	}
	return response, nil
}
func (s *RolloverScenarioService) Transition(ctx context.Context, id uint, request dto.RolloverScenarioTransitionRequest, actor util.Actor, requestID string) (dto.RolloverScenarioResponse, error) {
	if err := validateRequest(request); err != nil {
		return dto.RolloverScenarioResponse{}, err
	}
	scenario, err := s.scenarios.GetByID(ctx, id, false)
	if err != nil {
		return dto.RolloverScenarioResponse{}, util.NotFound("rollover scenario")
	}
	from, to := constants.ScenarioState(scenario.ScenarioState), constants.ScenarioState(request.ToState)
	if to != constants.ScenarioVerified {
		if err := requireScenarioOwnership(actor, scenario); err != nil {
			return dto.RolloverScenarioResponse{}, err
		}
	}
	if !constants.CanTransitionScenario(from, to) {
		return dto.RolloverScenarioResponse{}, util.NewError(http.StatusConflict, util.CodeStateTransition, "illegal scenario transition from "+scenario.ScenarioState+" to "+request.ToState)
	}
	if to == constants.ScenarioVerified && !scenario.ReviewerSeparated(actor.UserID) {
		return dto.RolloverScenarioResponse{}, util.NewError(http.StatusConflict, util.CodeReviewerConflict, "scenario creator cannot verify their own simulation")
	}
	if to == constants.ScenarioReady {
		reconciliation, reconcileErr := s.buildReconciliation(ctx, scenario)
		if reconcileErr != nil {
			return dto.RolloverScenarioResponse{}, reconcileErr
		}
		if !reconciliation.ReadyToMark {
			codes := make([]string, 0, len(reconciliation.Blocking))
			for _, item := range reconciliation.Blocking {
				codes = append(codes, item.ServiceCode)
			}

			return dto.RolloverScenarioResponse{}, util.NewError(http.StatusConflict, util.CodeStateTransition, "rollover cannot be marked ready until every service receipt reconciles; blocking services: "+strings.Join(codes, ", "))
		}
	}
	updates := map[string]any{}
	if to == constants.ScenarioVerified {
		updates["verified_by"] = actor.UserID
		updates["verified_by_name"] = actor.Username
	}
	if to == constants.ScenarioRollback {
		updates["rollback_record"] = strings.TrimSpace(request.Comment)
	}
	before := scenario
	err = s.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		changed, transitionErr := s.scenarios.Transition(txCtx, id, scenario.ScenarioState, request.ToState, updates)
		if transitionErr != nil {
			return transitionErr
		}
		if !changed {
			return util.NewError(http.StatusConflict, util.CodeConflict, "scenario state changed concurrently")
		}
		scenario.ScenarioState = request.ToState
		if to == constants.ScenarioVerified {
			scenario.VerifiedBy = &actor.UserID
			scenario.VerifiedByName = actor.Username
		}
		if to == constants.ScenarioRollback {
			scenario.RollbackRecord = strings.TrimSpace(request.Comment)
		}
		return recordAudit(txCtx, s.audits, actor, requestID, "rollover_scenario", id, "transition", before, scenario, scenario.InputHash, scenario.AlgorithmVersion, &scenario.SimulationTime, 0, request.Comment)
	})
	if err != nil {
		return dto.RolloverScenarioResponse{}, err
	}
	return s.Get(ctx, id)
}
func (s *RolloverScenarioService) Replay(ctx context.Context, id uint, actor util.Actor, requestID string) (dto.RolloverScenarioResponse, error) {
	scenario, err := s.scenarios.GetByID(ctx, id, false)
	if err != nil {
		return dto.RolloverScenarioResponse{}, util.NotFound("rollover scenario")
	}
	if err := requireScenarioOwnership(actor, scenario); err != nil {
		return dto.RolloverScenarioResponse{}, err
	}
	if scenario.ScenarioState == "draft" {
		return dto.RolloverScenarioResponse{}, util.NewError(http.StatusConflict, util.CodeStateTransition, "draft scenario has no stored result to replay")
	}
	snapshot, err := algorithm.DecodeSnapshot(scenario.InputSnapshot)
	if err != nil {
		return dto.RolloverScenarioResponse{}, util.WrapError(http.StatusUnprocessableEntity, util.CodeValidation, "frozen scenario snapshot is invalid", err)
	}
	result, err := algorithm.Simulate(snapshot)
	if err != nil {
		return dto.RolloverScenarioResponse{}, util.WrapError(http.StatusUnprocessableEntity, util.CodeValidation, "scenario replay failed", err)
	}
	affectedJSON, _ := encode(result.AffectedServices)
	pathsJSON, _ := encode(result.BrokenPaths)
	evidenceJSON, _ := encode(result.Evidence)
	passed := affectedJSON == scenario.AffectedServicesJSON && pathsJSON == scenario.BrokenPathsJSON && evidenceJSON == scenario.PathEvidenceJSON && result.Explanation == scenario.Explanation
	after := scenario
	after.ReplayVerified = passed
	err = s.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		if updateErr := s.scenarios.SetReplayVerified(txCtx, id, passed); updateErr != nil {
			return util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to store replay evidence", updateErr)
		}
		return recordAudit(txCtx, s.audits, actor, requestID, "rollover_scenario", id, "replay", scenario, after, scenario.InputHash, scenario.AlgorithmVersion, &scenario.SimulationTime, 0, result.Explanation)
	})
	if err != nil {
		return dto.RolloverScenarioResponse{}, err
	}
	if !passed {
		return dto.RolloverScenarioResponse{}, util.NewError(http.StatusConflict, util.CodeConflict, "replay differs from frozen historical result")
	}
	return s.Get(ctx, id)
}
func (s *RolloverScenarioService) Compare(ctx context.Context, id, otherID uint) (map[string]any, error) {
	first, err := s.scenarios.GetByID(ctx, id, false)
	if err != nil {
		return nil, util.NotFound("first rollover scenario")
	}
	second, err := s.scenarios.GetByID(ctx, otherID, false)
	if err != nil {
		return nil, util.NotFound("second rollover scenario")
	}
	var firstAffected, secondAffected []algorithm.AffectedService
	var firstPaths, secondPaths []algorithm.BrokenPath
	_ = json.Unmarshal([]byte(first.AffectedServicesJSON), &firstAffected)
	_ = json.Unmarshal([]byte(second.AffectedServicesJSON), &secondAffected)
	_ = json.Unmarshal([]byte(first.BrokenPathsJSON), &firstPaths)
	_ = json.Unmarshal([]byte(second.BrokenPathsJSON), &secondPaths)
	return map[string]any{"first_id": first.ID, "second_id": second.ID, "same_algorithm": first.AlgorithmVersion == second.AlgorithmVersion, "same_input": first.InputHash == second.InputHash, "summary": algorithm.Compare(algorithm.Result{AffectedServices: firstAffected, BrokenPaths: firstPaths}, algorithm.Result{AffectedServices: secondAffected, BrokenPaths: secondPaths})}, nil
}

// deliveryFailureMarker lets a service owner exercise the isolated-retry path
// deterministically without contacting a production reporting system.
const deliveryFailureMarker = "SIMULATE_DELIVERY_FAILURE"

// deliverReceipt models the per-receipt reporting channel; one failure never blocks another.
func deliverReceipt(note string) error {
	if strings.Contains(note, deliveryFailureMarker) {
		return fmt.Errorf("reporting channel rejected receipt for this service")
	}
	return nil
}

func brokenServiceIDs(scenario model.RolloverScenario) map[uint]bool {
	var affected []algorithm.AffectedService
	_ = json.Unmarshal([]byte(scenario.AffectedServicesJSON), &affected)
	broken := map[uint]bool{}
	for _, item := range affected {
		broken[item.ServiceID] = true
	}
	return broken
}

func requireReceiptOwnership(actor util.Actor, service model.DependentService) error {
	if actor.Role == string(constants.RoleAdmin) || actor.Role == string(constants.RolePKIOperator) ||
		(actor.Role == string(constants.RoleServiceOwner) && actor.Team == service.OwnerTeam) {
		return nil
	}
	return util.NewError(http.StatusForbidden, util.CodeForbidden, "only the owning service team may report this receipt")
}

func reconciliationReason(status constants.ReconciliationStatus, broken bool) string {
	switch status {
	case constants.ReconciliationReconciled:
		if broken {
			return "推演曾预测断裂；服务报已换到新根且经独立复核确认。"
		}
		return "推演未预测断裂，服务报已换到新根，两侧一致。"
	case constants.ReconciliationMismatchPendingReview:
		return "推演预测该服务信任路径会断裂，但服务侧报已换好；已挂起，等待安全复核员复核。"
	case constants.ReconciliationOutstanding:
		return "服务侧尚未完成换到新根，轮换不能判成就绪。"
	case constants.ReconciliationLegacyMissing:
		return "旧推演原本没有回执，升级时已按服务补齐占位；对不上的一项，请服务负责人补报。"
	case constants.ReconciliationDeliveryFailed:
		return "回执报送失败，已隔离；可对该服务单独重试，不影响其他服务。"
	default:
		return ""
	}
}

func (s *RolloverScenarioService) Reconciliation(ctx context.Context, id uint) (dto.ReceiptReconciliationResponse, error) {
	scenario, err := s.scenarios.GetByID(ctx, id, false)
	if err != nil {
		return dto.ReceiptReconciliationResponse{}, util.NotFound("rollover scenario")
	}
	return s.buildReconciliation(ctx, scenario)
}

func (s *RolloverScenarioService) buildReconciliation(ctx context.Context, scenario model.RolloverScenario) (dto.ReceiptReconciliationResponse, error) {
	allServices, err := s.services.All(ctx)
	if err != nil {
		return dto.ReceiptReconciliationResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load dependency graph", err)
	}
	receipts, err := s.scenarios.ListReceipts(ctx, scenario.ID)
	if err != nil {
		return dto.ReceiptReconciliationResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load migration receipts", err)
	}
	byService := map[uint]model.MigrationReceipt{}
	for _, receipt := range receipts {
		byService[receipt.ServiceID] = receipt
	}
	broken := brokenServiceIDs(scenario)
	response := dto.ReceiptReconciliationResponse{ScenarioID: scenario.ID, ScenarioState: scenario.ScenarioState, TotalServices: len(allServices), Counts: map[string]int{}, Items: []dto.ServiceReconciliation{}, Blocking: []dto.ServiceReconciliation{}}
	for _, svc := range allServices {
		receipt, exists := byService[svc.ID]
		migrationState, deliveryState, reviewStatus, reported := "", "", "", false
		var receiptPtr *dto.MigrationReceiptResponse
		if exists {
			migrationState, deliveryState, reviewStatus, reported = receipt.MigrationState, receipt.DeliveryState, receipt.ReviewStatus, !receipt.LegacyBackfilled
			r := dto.NewMigrationReceiptResponse(receipt)
			receiptPtr = &r
		}
		status := constants.ReconcileReceipt(broken[svc.ID], receipt.LegacyBackfilled, reported, migrationState, deliveryState, reviewStatus)
		response.Counts[string(status)]++
		item := dto.ServiceReconciliation{ServiceID: svc.ID, ServiceCode: svc.ServiceCode, PredictedBroken: broken[svc.ID], HasReceipt: reported, Receipt: receiptPtr, Status: string(status), Reason: reconciliationReason(status, broken[svc.ID])}
		response.Items = append(response.Items, item)
		if status.ReadyBlocked() {
			response.Blocking = append(response.Blocking, item)
		}
	}
	response.ReadyToMark = len(response.Blocking) == 0
	return response, nil
}

func (s *RolloverScenarioService) loadScenarioAndService(ctx context.Context, scenarioID, serviceID uint) (model.RolloverScenario, model.DependentService, error) {
	scenario, err := s.scenarios.GetByID(ctx, scenarioID, false)
	if err != nil {
		return model.RolloverScenario{}, model.DependentService{}, util.NotFound("rollover scenario")
	}
	service, err := s.services.GetByID(ctx, serviceID, false)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.RolloverScenario{}, model.DependentService{}, util.NotFound("dependent service")
		}
		return model.RolloverScenario{}, model.DependentService{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load dependent service", err)
	}
	return scenario, service, nil
}

func (s *RolloverScenarioService) getPriorReceipt(ctx context.Context, scenarioID, serviceID uint) (model.MigrationReceipt, error) {
	prior, err := s.scenarios.GetReceipt(ctx, scenarioID, serviceID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.MigrationReceipt{}, nil
	}
	return prior, err
}

// persistReceipt writes the receipt plus its audit record in one transaction.
func (s *RolloverScenarioService) persistReceipt(ctx context.Context, receipt *model.MigrationReceipt, before any, actor util.Actor, requestID, action, summary string, scenario model.RolloverScenario) error {
	return s.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.scenarios.UpsertReceipt(txCtx, receipt); err != nil {
			return err
		}
		return recordAudit(txCtx, s.audits, actor, requestID, "migration_receipt", receipt.ID, action, before, *receipt, scenario.InputHash, scenario.AlgorithmVersion, &scenario.SimulationTime, 0, summary)
	})
}

func applyDelivery(receipt *model.MigrationReceipt, deliveryErr error, at time.Time) {
	receipt.DeliveryAttempts++
	if deliveryErr != nil {
		receipt.DeliveryState, receipt.LastDeliveryError, receipt.DeliveredAt = string(constants.DeliveryFailed), deliveryErr.Error(), nil
		return
	}
	receipt.DeliveryState, receipt.LastDeliveryError = string(constants.DeliveryDelivered), ""
	delivered := at
	receipt.DeliveredAt = &delivered
}

// SubmitReceipt records one owner's progress; a failed delivery is stored for an isolated retry.
func (s *RolloverScenarioService) SubmitReceipt(ctx context.Context, scenarioID uint, request dto.SubmitMigrationReceiptRequest, actor util.Actor, requestID string) (dto.MigrationReceiptResponse, error) {
	if err := validateRequest(request); err != nil {
		return dto.MigrationReceiptResponse{}, err
	}
	scenario, service, err := s.loadScenarioAndService(ctx, scenarioID, request.ServiceID)
	if err != nil {
		return dto.MigrationReceiptResponse{}, err
	}
	if err := requireReceiptOwnership(actor, service); err != nil {
		return dto.MigrationReceiptResponse{}, err
	}
	if scenario.ScenarioState == string(constants.ScenarioDraft) {
		return dto.MigrationReceiptResponse{}, util.NewError(http.StatusConflict, util.CodeStateTransition, "run the simulation before reporting receipts")
	}
	anchorIDs := uniqueIDs(request.TrustAnchorIDs)
	if anchors, anchorErr := s.anchors.GetByIDs(ctx, anchorIDs); anchorErr != nil || len(anchors) != len(anchorIDs) {
		return dto.MigrationReceiptResponse{}, util.NewError(http.StatusBadRequest, util.CodeValidation, "one or more client trust anchors do not exist")
	}
	trustJSON, _ := encode(anchorIDs)
	prior, err := s.getPriorReceipt(ctx, scenarioID, request.ServiceID)
	if err != nil {
		return dto.MigrationReceiptResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load prior receipt", err)
	}
	// A "migrated" claim on a service predicted to break is contradictory and held;
	// an unfinished report clears any stale hold. A prior independent clearance persists.
	reviewStatus := constants.ReviewNotRequired
	if request.MigrationState == string(constants.MigrationMigrated) && brokenServiceIDs(scenario)[request.ServiceID] && constants.ReviewStatus(prior.ReviewStatus) != constants.ReviewCleared {
		reviewStatus = constants.ReviewPending
	} else if constants.ReviewStatus(prior.ReviewStatus) == constants.ReviewCleared {
		reviewStatus = constants.ReviewCleared
	}
	now := s.now()
	receipt := model.MigrationReceipt{ScenarioID: scenarioID, ServiceID: request.ServiceID, ServiceCode: service.ServiceCode, MigrationState: request.MigrationState, TrustAnchorIDs: trustJSON, Note: strings.TrimSpace(request.Note), DeliveryState: string(constants.DeliveryPending), ReviewStatus: string(reviewStatus), ReportedBy: actor.UserID, ReportedByName: actor.Username, CreatedAt: now, UpdatedAt: now}
	if prior.ID != 0 {
		receipt.CreatedAt = prior.CreatedAt
		if reviewStatus == constants.ReviewCleared {
			receipt.ReviewedBy, receipt.ReviewedByName, receipt.ReviewedAt, receipt.ReviewComment = prior.ReviewedBy, prior.ReviewedByName, prior.ReviewedAt, prior.ReviewComment
		}
	}
	applyDelivery(&receipt, deliverReceipt(request.Note), now)
	var before any
	if prior.ID != 0 {
		before = prior
	}
	if err := s.persistReceipt(ctx, &receipt, before, actor, requestID, "submit", "service "+service.ServiceCode+" reported "+request.MigrationState, scenario); err != nil {
		return dto.MigrationReceiptResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to record migration receipt", err)
	}
	return dto.NewMigrationReceiptResponse(receipt), nil
}

// RetryReceipt retries delivery for exactly one failed receipt.
func (s *RolloverScenarioService) RetryReceipt(ctx context.Context, scenarioID, serviceID uint, request dto.RetryMigrationReceiptRequest, actor util.Actor, requestID string) (dto.MigrationReceiptResponse, error) {
	scenario, service, err := s.loadScenarioAndService(ctx, scenarioID, serviceID)
	if err != nil {
		return dto.MigrationReceiptResponse{}, err
	}
	if err := requireReceiptOwnership(actor, service); err != nil {
		return dto.MigrationReceiptResponse{}, err
	}
	receipt, err := s.scenarios.GetReceipt(ctx, scenarioID, serviceID)
	if err != nil {
		return dto.MigrationReceiptResponse{}, util.NewError(http.StatusNotFound, util.CodeNotFound, "this service has no receipt to retry")
	}
	if receipt.DeliveryState != string(constants.DeliveryFailed) {
		return dto.MigrationReceiptResponse{}, util.NewError(http.StatusConflict, util.CodeConflict, "only receipts that failed delivery can be retried")
	}
	if note := strings.TrimSpace(request.Note); note != "" {
		receipt.Note = note
	}
	before := receipt
	applyDelivery(&receipt, deliverReceipt(receipt.Note), s.now())
	if err := s.persistReceipt(ctx, &receipt, before, actor, requestID, "retry_delivery", "isolated delivery retry for "+service.ServiceCode, scenario); err != nil {
		return dto.MigrationReceiptResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to retry receipt delivery", err)
	}
	return dto.NewMigrationReceiptResponse(receipt), nil
}

// ReviewReceipt lets an independent reviewer clear or reject a held mismatch.
func (s *RolloverScenarioService) ReviewReceipt(ctx context.Context, scenarioID, serviceID uint, request dto.ReviewMigrationReceiptRequest, actor util.Actor, requestID string) (dto.MigrationReceiptResponse, error) {
	if err := validateRequest(request); err != nil {
		return dto.MigrationReceiptResponse{}, err
	}
	if actor.Role != string(constants.RoleAdmin) && actor.Role != string(constants.RoleSecurityReviewer) {
		return dto.MigrationReceiptResponse{}, util.NewError(http.StatusForbidden, util.CodeForbidden, "only an independent security reviewer may resolve a held mismatch")
	}
	scenario, err := s.scenarios.GetByID(ctx, scenarioID, false)
	if err != nil {
		return dto.MigrationReceiptResponse{}, util.NotFound("rollover scenario")
	}
	if !brokenServiceIDs(scenario)[serviceID] {
		return dto.MigrationReceiptResponse{}, util.NewError(http.StatusBadRequest, util.CodeValidation, "simulation did not predict a break for this service")
	}
	receipt, err := s.scenarios.GetReceipt(ctx, scenarioID, serviceID)
	if err != nil {
		return dto.MigrationReceiptResponse{}, util.NewError(http.StatusNotFound, util.CodeNotFound, "this service has no receipt to review")
	}
	if receipt.ReportedBy == actor.UserID {
		return dto.MigrationReceiptResponse{}, util.NewError(http.StatusConflict, util.CodeReviewerConflict, "the reporter cannot independently review their own receipt")
	}
	if receipt.ReviewStatus != string(constants.ReviewPending) {
		return dto.MigrationReceiptResponse{}, util.NewError(http.StatusConflict, util.CodeConflict, "only held mismatches awaiting review can be resolved")
	}
	before := receipt
	now := s.now()
	receipt.ReviewStatus, receipt.ReviewedBy, receipt.ReviewedByName, receipt.ReviewedAt, receipt.ReviewComment, receipt.UpdatedAt = request.Decision, &actor.UserID, actor.Username, &now, strings.TrimSpace(request.Comment), now
	if err := s.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.scenarios.UpsertReceipt(txCtx, &receipt); err != nil {
			return err
		}
		return recordAudit(txCtx, s.audits, actor, requestID, "migration_receipt", receipt.ID, "independent_review", before, receipt, "", "", nil, 0, "reviewer "+request.Decision+" mismatch")
	}); err != nil {
		return dto.MigrationReceiptResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to record review decision", err)
	}
	return dto.NewMigrationReceiptResponse(receipt), nil
}

// BackfillReceipts adds legacy placeholders only for missing (scenario, service)
// rows; existing owner receipts are never overwritten.
func (s *RolloverScenarioService) BackfillReceipts(ctx context.Context, scenarioID uint, actor util.Actor, requestID string) (dto.ReceiptReconciliationResponse, error) {
	scenario, err := s.scenarios.GetByID(ctx, scenarioID, false)
	if err != nil {
		return dto.ReceiptReconciliationResponse{}, util.NotFound("rollover scenario")
	}
	if scenario.ScenarioState == string(constants.ScenarioDraft) {
		return dto.ReceiptReconciliationResponse{}, util.NewError(http.StatusConflict, util.CodeStateTransition, "simulate the scenario before backfilling receipts")
	}
	allServices, err := s.services.All(ctx)
	if err != nil {
		return dto.ReceiptReconciliationResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load dependency graph", err)
	}
	existing, err := s.scenarios.ListReceipts(ctx, scenarioID)
	if err != nil {
		return dto.ReceiptReconciliationResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load migration receipts", err)
	}
	present := map[uint]bool{}
	for _, receipt := range existing {
		present[receipt.ServiceID] = true
	}
	now := s.now()
	placeholders := []model.MigrationReceipt{}
	for _, svc := range allServices {
		if present[svc.ID] {
			continue
		}
		placeholders = append(placeholders, model.MigrationReceipt{ScenarioID: scenarioID, ServiceID: svc.ID, ServiceCode: svc.ServiceCode, MigrationState: string(constants.MigrationNotStarted), TrustAnchorIDs: "[]", Note: "legacy scenario: receipt backfilled on upgrade", DeliveryState: string(constants.DeliveryPending), ReviewStatus: string(constants.ReviewNotRequired), LegacyBackfilled: true, ReportedBy: actor.UserID, ReportedByName: actor.Username, CreatedAt: now, UpdatedAt: now})
	}
	created, err := s.scenarios.CreateMissingReceipts(ctx, placeholders)
	if err != nil {
		return dto.ReceiptReconciliationResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to backfill legacy receipts", err)
	}
	_ = s.transactions.WithinTransaction(ctx, func(txCtx context.Context) error {
		return recordAudit(txCtx, s.audits, actor, requestID, "migration_receipt", scenarioID, "backfill_legacy", nil, map[string]any{"created": created}, scenario.InputHash, scenario.AlgorithmVersion, &scenario.SimulationTime, 0, fmt.Sprintf("backfilled %d missing legacy receipt placeholders", created))
	})
	return s.buildReconciliation(ctx, scenario)
}
