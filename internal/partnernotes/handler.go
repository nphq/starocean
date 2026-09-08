package partnernotes

import (
	"context"
	"database/sql"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/nphq/starocean/internal/models"
	"github.com/nphq/starocean/internal/shared"
)

type Handler struct {
	db *sql.DB
}

func New(db *sql.DB) *Handler {
	return &Handler{db: db}
}

type noteInput struct {
	PartnerType  string `json:"partner_type"`
	PartnerID    string `json:"partner_id"`
	NoteType     string `json:"note_type"`
	Content      string `json:"content"`
	NextFollowUp string `json:"next_follow_up"`
}

func (h *Handler) ListByPartner(c *gin.Context) {
	partnerType := c.Param("partnerType")
	partnerID, err := uuid.Parse(c.Param("partnerId"))
	if err != nil {
		shared.JSONBadRequest(c, "无效的ID")
		return
	}

	notes, err := GetNotesByPartner(c.Request.Context(), h.db, partnerType, partnerID)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONOK(c, shared.EmptySlice(notes))
}

func (h *Handler) Create(c *gin.Context) {
	var in noteInput
	if !shared.BindJSON(c, &in) {
		return
	}
	partnerID, err := uuid.Parse(in.PartnerID)
	if err != nil {
		shared.JSONBadRequest(c, "无效的ID")
		return
	}
	if in.Content == "" {
		shared.JSONBadRequest(c, "内容不能为空")
		return
	}

	var nextFollowUp interface{}
	if in.NextFollowUp != "" {
		t, err := time.Parse("2006-01-02", in.NextFollowUp)
		if err == nil {
			nextFollowUp = t
		}
	}

	ctx := c.Request.Context()
	id := uuid.New()
	_, err = h.db.ExecContext(ctx, `
		INSERT INTO partner_notes (id, partner_type, partner_id, note_type, content, next_follow_up)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, id, in.PartnerType, partnerID, in.NoteType, in.Content, nextFollowUp)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}

	row := h.db.QueryRowContext(ctx, `
		SELECT id, partner_type, partner_id, note_type, content, next_follow_up, created_at, company_id
		FROM partner_notes WHERE id = $1
	`, id)

	n, err := scanNote(row)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}

	shared.JSONCreated(c, n)
}

func (h *Handler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.JSONBadRequest(c, "无效的ID")
		return
	}
	_, err = h.db.ExecContext(c.Request.Context(), "DELETE FROM partner_notes WHERE id = $1", id)
	if err != nil {
		shared.JSONInternal(c, err)
		return
	}
	shared.JSONOK(c, gin.H{"ok": true})
}

func GetNotesByPartner(ctx context.Context, db *sql.DB, partnerType string, partnerID uuid.UUID) ([]models.PartnerNote, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT pn.id, pn.partner_type, pn.partner_id, pn.note_type, pn.content,
			   pn.next_follow_up, pn.created_at, COALESCE(pn.company_id, 'default')
		FROM partner_notes pn
		WHERE pn.partner_type = $1 AND pn.partner_id = $2
		ORDER BY pn.created_at DESC
	`, partnerType, partnerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []models.PartnerNote
	for rows.Next() {
		var n models.PartnerNote
		var nextFollowUp sql.NullTime
		if err := rows.Scan(&n.ID, &n.PartnerType, &n.PartnerID, &n.NoteType, &n.Content,
			&nextFollowUp, &n.CreatedAt, &n.CompanyID); err != nil {
			continue
		}
		if nextFollowUp.Valid {
			n.NextFollowUp = &nextFollowUp.Time
		}
		notes = append(notes, n)
	}
	return notes, nil
}

func scanNote(row interface{ Scan(...interface{}) error }) (models.PartnerNote, error) {
	var n models.PartnerNote
	var nextFollowUp sql.NullTime
	err := row.Scan(&n.ID, &n.PartnerType, &n.PartnerID, &n.NoteType, &n.Content,
		&nextFollowUp, &n.CreatedAt, &n.CompanyID)
	if err != nil {
		return n, err
	}
	if nextFollowUp.Valid {
		n.NextFollowUp = &nextFollowUp.Time
	}
	return n, nil
}
