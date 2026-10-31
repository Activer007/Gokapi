package database

import (
	"context"

	"github.com/Activer007/SupaShare/internal/models"
)

func InsertDelivery(ctx context.Context, delivery models.Delivery) (bool, error) {
	return db.InsertDelivery(ctx, delivery)
}

func ReadDelivery(ctx context.Context, token string) (models.Delivery, bool, error) {
	return db.ReadDelivery(ctx, token)
}

func ListDeliveries(ctx context.Context, ownerID int) ([]models.Delivery, error) {
	return db.ListDeliveries(ctx, ownerID)
}

func RevokeDelivery(ctx context.Context, token string, ownerID int, now int64) (bool, error) {
	return db.RevokeDelivery(ctx, token, ownerID, now)
}
