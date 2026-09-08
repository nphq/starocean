package products

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/nphq/starocean/internal/models"
)

type PriceEngine struct {
	db *sql.DB
}

func NewPriceEngine(db *sql.DB) *PriceEngine {
	return &PriceEngine{db: db}
}

// ResolvePrice returns the final unit price for a given customer + product + quantity.
// Priority: customer exclusive price > tier price > default sale price.
func (e *PriceEngine) ResolvePrice(ctx context.Context, customerID, productID uuid.UUID, quantity int32, defaultPrice decimal.Decimal) decimal.Decimal {
	// 1. Check customer exclusive price
	if customerID != uuid.Nil {
		var price decimal.Decimal
		now := time.Now()
		err := e.db.QueryRowContext(ctx,
			`SELECT unit_price FROM customer_product_prices
			 WHERE customer_id = $1 AND product_id = $2
			   AND (effective_from IS NULL OR effective_from <= $3)
			   AND (effective_to IS NULL OR effective_to >= $3)
			 LIMIT 1`, customerID, productID, now).Scan(&price)
		if err == nil {
			return price
		}
	}

	// 2. Check tier price
	var tierPrice decimal.Decimal
	err := e.db.QueryRowContext(ctx,
		`SELECT unit_price FROM product_price_tiers
		 WHERE product_id = $1 AND min_quantity <= $2 AND (max_quantity IS NULL OR max_quantity >= $2)
		 ORDER BY min_quantity DESC LIMIT 1`, productID, quantity).Scan(&tierPrice)
	if err == nil {
		return tierPrice
	}

	// 3. Default sale price
	return defaultPrice
}

func ListPriceTiers(ctx context.Context, db *sql.DB, productID uuid.UUID) ([]models.PriceTier, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, product_id, min_quantity, COALESCE(max_quantity, 0), unit_price, created_at,
		        COALESCE(company_id, 'default')
		 FROM product_price_tiers WHERE product_id = $1 ORDER BY min_quantity`, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tiers []models.PriceTier
	for rows.Next() {
		var t models.PriceTier
		if err := rows.Scan(&t.ID, &t.ProductID, &t.MinQuantity, &t.MaxQuantity, &t.UnitPrice, &t.CreatedAt, &t.CompanyID); err != nil {
			return nil, err
		}
		tiers = append(tiers, t)
	}
	return tiers, rows.Err()
}

func CreatePriceTier(ctx context.Context, db *sql.DB, t *models.PriceTier) error {
	t.ID = uuid.New()
	var maxQty interface{}
	if t.MaxQuantity > 0 {
		maxQty = t.MaxQuantity
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO product_price_tiers (id, product_id, min_quantity, max_quantity, unit_price, company_id)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		t.ID, t.ProductID, t.MinQuantity, maxQty, t.UnitPrice, t.CompanyID)
	return err
}
