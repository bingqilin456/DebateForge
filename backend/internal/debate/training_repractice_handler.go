package debate

import (
	"context"
	"errors"

	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/gin-gonic/gin"
)

type trainingRepracticeRequest struct {
	RequestID string `json:"client_request_id"`
	MessageID string `json:"message_id"`
	Start     int    `json:"start_cp"`
	End       int    `json:"end_cp"`
	Quote     string `json:"quote"`
	Goal      string `json:"goal"`
	Rewrite   string `json:"rewrite"`
}

type trainingRepracticeVersion struct {
	ID       string             `json:"id"`
	Attempt  uint64             `json:"attempt_version"`
	Status   string             `json:"status"`
	Rewrite  string             `json:"rewrite"`
	Feedback RepracticeFeedback `json:"feedback"`
}

func projectRepractice(v RepracticeVersion) trainingRepracticeVersion {
	return trainingRepracticeVersion{formatID(v.ID), v.Attempt, v.Status, v.Input.Rewrite, v.Feedback}
}

func (s *Service) EnableTrainingRepractice(repo *Repository, generate func(context.Context, RepracticeInput) (RepracticeFeedback, error)) {
	s.trainingRepractice = NewRepractice(func(ctx context.Context, uid, id uint64) (Detail, error) {
		detail, err := s.Detail(ctx, uid, id)
		if err == nil && detail.Training == nil {
			return Detail{}, ErrNotFound
		}
		return detail, err
	}, generate)
	s.trainingRepractice.store = repo.withTrainingRepractice
}

func (h *Handler) registerTrainingRepractice(group *gin.RouterGroup, a *auth.Handler) {
	if h.service == nil || h.service.trainingRepractice == nil {
		return
	}
	r := h.service.trainingRepractice
	group.GET("/debates/:id/repractice", a.RequireAuth(), func(c *gin.Context) {
		id, err := ParseID(c.Param("id"))
		if err != nil {
			serviceError(c, err)
			return
		}
		current, _ := auth.CurrentUserFrom(c)
		list, err := r.List(c.Request.Context(), current.User.ID, id)
		if err != nil {
			writeRepracticeError(c, err)
			return
		}
		items := make([]trainingRepracticeVersion, 0, len(list))
		for _, item := range list {
			items = append(items, projectRepractice(item))
		}
		writeData(c, 200, items)
	})
	group.POST("/debates/:id/repractice", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.limitGeneration, func(c *gin.Context) {
		id, err := ParseID(c.Param("id"))
		if err != nil {
			serviceError(c, err)
			return
		}
		var in trainingRepracticeRequest
		if !decode(c, &in) {
			return
		}
		msg, err := ParseID(in.MessageID)
		if err != nil {
			serviceError(c, err)
			return
		}
		current, _ := auth.CurrentUserFrom(c)
		version, err := r.Submit(c.Request.Context(), current.User.ID, id, RepracticeInput{
			RequestID: in.RequestID, MessageID: msg, Start: in.Start, End: in.End, Quote: in.Quote, Goal: in.Goal, Rewrite: in.Rewrite,
		})
		if err != nil {
			writeRepracticeError(c, err)
			return
		}
		writeData(c, 200, projectRepractice(version))
	})
	group.POST("/debates/:id/repractice/:version/retry", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.limitGeneration, func(c *gin.Context) {
		id, idErr := ParseID(c.Param("id"))
		version, versionErr := ParseID(c.Param("version"))
		if idErr != nil || versionErr != nil {
			serviceError(c, ErrInvalidRequest)
			return
		}
		var in struct {
			Attempt uint64 `json:"expected_attempt_version"`
		}
		if !decode(c, &in) {
			return
		}
		if in.Attempt == 0 {
			serviceError(c, ErrInvalidRequest)
			return
		}
		current, _ := auth.CurrentUserFrom(c)
		result, err := r.Retry(c.Request.Context(), current.User.ID, id, version, in.Attempt)
		if err != nil {
			writeRepracticeError(c, err)
			return
		}
		writeData(c, 200, projectRepractice(result))
	})
}

func writeRepracticeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrRepracticeSource):
		serviceError(c, ErrRoundConflict)
	case errors.Is(err, ErrRepracticeInput):
		serviceError(c, ErrInvalidRequest)
	case errors.Is(err, ErrRepracticeVersion):
		serviceError(c, ErrNotFound)
	default:
		serviceError(c, err)
	}
}
