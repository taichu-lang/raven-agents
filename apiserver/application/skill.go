package application

import (
	"context"
	"fmt"

	"github.com/taichu-lang/raven-agents/apiserver/domain"
	"github.com/taichu-lang/raven-agents/apiserver/infrastructure/skill"
)

// SkillService serves the skill marketplace.
type SkillService struct {
	store *skill.SkillStore
}

func NewSkillService(store *skill.SkillStore) *SkillService {
	return &SkillService{
		store: store,
	}
}

func (s *SkillService) List(ctx context.Context, from, limit int64) ([]domain.SkillItem, error) {
	if limit <= 0 {
		limit = domain.DefaultPaginationSize
	}

	if from < 0 {
		from = 0
	}

	models, err := s.store.List(ctx, from, limit)
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}

	items := make([]domain.SkillItem, 0, len(models))
	for _, model := range models {
		items = append(items, domain.SkillItem{
			ID:          model.ID,
			Name:        model.Name,
			Provider:    model.Provider,
			Icon:        model.Icon,
			Display:     model.Display,
			Version:     model.Version,
			Description: model.Description,
			Source:      model.Source,
			Metadata:    model.Metadata,
			CreatedAt:   model.CreatedAt,
		})
	}

	return items, nil
}
