package repository

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"pki-certificate-rollover-impact/backend/internal/model"
)

type ReceiptRepository interface {
	Create(context.Context, *model.Receipt) error
	GetByID(context.Context, uint) (model.Receipt, error)
	GetByScenarioAndService(context.Context, uint, uint) (model.Receipt, error)
	ListByScenario(context.Context, uint) ([]model.Receipt, error)
	ListByScenarioAndState(context.Context, uint, string) ([]model.Receipt, error)
	Update(context.Context, uint, map[string]any) error
	CountByScenario(context.Context, uint) (int64, error)
	DeleteByScenario(context.Context, uint) error
}

type receiptRepository struct{ db *gorm.DB }

func NewReceiptRepository(db *gorm.DB) ReceiptRepository {
	return &receiptRepository{db: db}
}

func (r *receiptRepository) Create(ctx context.Context, receipt *model.Receipt) error {
	if err := scopedDB(ctx, r.db).Create(receipt).Error; err != nil {
		return fmt.Errorf("create receipt: %w", err)
	}
	return nil
}

func (r *receiptRepository) GetByScenarioAndService(ctx context.Context, scenarioID, serviceID uint) (model.Receipt, error) {
	var receipt model.Receipt
	if err := scopedDB(ctx, r.db).Where("scenario_id = ? AND service_id = ?", scenarioID, serviceID).First(&receipt).Error; err != nil {
		return model.Receipt{}, fmt.Errorf("find receipt for scenario %d service %d: %w", scenarioID, serviceID, err)
	}
	return receipt, nil
}

func (r *receiptRepository) GetByID(ctx context.Context, id uint) (model.Receipt, error) {
	var receipt model.Receipt
	if err := scopedDB(ctx, r.db).First(&receipt, id).Error; err != nil {
		return model.Receipt{}, fmt.Errorf("find receipt %d: %w", id, err)
	}
	return receipt, nil
}

func (r *receiptRepository) ListByScenario(ctx context.Context, scenarioID uint) ([]model.Receipt, error) {
	var receipts []model.Receipt
	if err := scopedDB(ctx, r.db).Where("scenario_id = ?", scenarioID).Order("service_id ASC").Find(&receipts).Error; err != nil {
		return nil, fmt.Errorf("list receipts for scenario %d: %w", scenarioID, err)
	}
	return receipts, nil
}

func (r *receiptRepository) ListByScenarioAndState(ctx context.Context, scenarioID uint, state string) ([]model.Receipt, error) {
	var receipts []model.Receipt
	if err := scopedDB(ctx, r.db).Where("scenario_id = ? AND receipt_state = ?", scenarioID, state).Order("service_id ASC").Find(&receipts).Error; err != nil {
		return nil, fmt.Errorf("list receipts for scenario %d in state %s: %w", scenarioID, state, err)
	}
	return receipts, nil
}

func (r *receiptRepository) Update(ctx context.Context, id uint, updates map[string]any) error {
	updates["updated_at"] = time.Now().UTC()
	result := scopedDB(ctx, r.db).Model(&model.Receipt{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update receipt: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *receiptRepository) CountByScenario(ctx context.Context, scenarioID uint) (int64, error) {
	var count int64
	if err := scopedDB(ctx, r.db).Model(&model.Receipt{}).Where("scenario_id = ?", scenarioID).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count receipts for scenario %d: %w", scenarioID, err)
	}
	return count, nil
}

func (r *receiptRepository) DeleteByScenario(ctx context.Context, scenarioID uint) error {
	if err := scopedDB(ctx, r.db).Where("scenario_id = ?", scenarioID).Delete(&model.Receipt{}).Error; err != nil {
		return fmt.Errorf("delete receipts for scenario %d: %w", scenarioID, err)
	}
	return nil
}
