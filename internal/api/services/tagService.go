package services

import (
	"RadioPump/internal/models"
	"RadioPump/internal/repository"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalidTagName = errors.New("некорректное имя тега")
	ErrTagExists      = errors.New("тег с таким именем уже существует")
	ErrTagInUse       = errors.New("тег используется в конфигурации волны")
)

// TagCatalog — HTTP-независимый контракт управления справочником тегов.
type TagCatalog interface {
	GetAll() ([]models.Tag, error)
	Create(name string) (*models.Tag, error)
	Update(id uint, name string) (*models.Tag, error)
	Delete(id uint) error
}

// TagService реализует правила имён и ограничения config.yaml поверх repository.
type TagService struct {
	repo             repository.TagRepository
	stations         StationInvalidator
	configuredByName map[string]struct{}
}

var _ TagCatalog = (*TagService)(nil)

// NewTagService получает только имена, используемые волнами, и не зависит от config package.
func NewTagService(repo repository.TagRepository, stations StationInvalidator, configuredNames []string) *TagService {
	configured := make(map[string]struct{}, len(configuredNames))
	for _, name := range configuredNames {
		if normalized, err := normalizeTagName(name); err == nil {
			configured[normalized] = struct{}{}
		}
	}
	return &TagService{repo: repo, stations: stations, configuredByName: configured}
}

// GetAll отдаёт repository-порядок без добавления виртуальных значений.
func (s *TagService) GetAll() ([]models.Tag, error) {
	return s.repo.GetAllTags()
}

// Create нормализует имя и запрещает второй вариант того же тега в другом регистре.
func (s *TagService) Create(name string) (*models.Tag, error) {
	normalized, err := normalizeTagName(name)
	if err != nil {
		return nil, err
	}
	if exists, err := s.nameExists(normalized, 0); err != nil || exists {
		if exists {
			return nil, ErrTagExists
		}
		return nil, err
	}
	tag := &models.Tag{Name: normalized}
	if err := s.repo.CreateTag(tag); err != nil {
		// Повторная проверка закрывает гонку двух одновременных create-запросов.
		if exists, lookupErr := s.nameExists(normalized, 0); lookupErr == nil && exists {
			return nil, ErrTagExists
		}
		return nil, err
	}
	return tag, nil
}

// Update сохраняет стабильный ID; имя, указанное в config.yaml, менять нельзя.
func (s *TagService) Update(id uint, name string) (*models.Tag, error) {
	current, err := s.repo.GetTagByID(id)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeTagName(name)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(strings.TrimSpace(current.Name), normalized) {
		return current, nil
	}
	if s.isConfigured(current.Name) {
		return nil, ErrTagInUse
	}
	if exists, err := s.nameExists(normalized, id); err != nil || exists {
		if exists {
			return nil, ErrTagExists
		}
		return nil, err
	}
	current.Name = normalized
	if err := s.repo.UpdateTag(current); err != nil {
		// Повторная проверка превращает concurrent UNIQUE conflict в стабильную ошибку API.
		if exists, lookupErr := s.nameExists(normalized, id); lookupErr == nil && exists {
			return nil, ErrTagExists
		}
		return nil, err
	}
	s.markStationsDirty()
	return current, nil
}

// Delete разрешает удалять базовые и пользовательские теги, кроме активных в волнах.
func (s *TagService) Delete(id uint) error {
	current, err := s.repo.GetTagByID(id)
	if err != nil {
		return err
	}
	if s.isConfigured(current.Name) {
		return ErrTagInUse
	}
	if err := s.repo.DeleteTag(id); err != nil {
		return err
	}
	s.markStationsDirty()
	return nil
}

// nameExists сравнивает нормализованные значения независимо от особенностей SQLite collation.
func (s *TagService) nameExists(name string, exceptID uint) (bool, error) {
	tags, err := s.repo.GetAllTags()
	if err != nil {
		return false, err
	}
	for _, tag := range tags {
		if tag.ID != exceptID && strings.EqualFold(strings.TrimSpace(tag.Name), name) {
			return true, nil
		}
	}
	return false, nil
}

// isConfigured защищает строковые ссылки текущих волн от незаметной поломки.
func (s *TagService) isConfigured(name string) bool {
	normalized, err := normalizeTagName(name)
	if err != nil {
		return false
	}
	_, exists := s.configuredByName[normalized]
	return exists
}

// normalizeTagName приводит имена к нижнему регистру и единичным пробелам.
// Управляющие символы запрещены, длина ограничена 64 Unicode-символами.
func normalizeTagName(name string) (string, error) {
	normalized := strings.ToLower(strings.Join(strings.Fields(name), " "))
	if normalized == "" || utf8.RuneCountInString(normalized) > 64 {
		return "", ErrInvalidTagName
	}
	for _, symbol := range normalized {
		if unicode.IsControl(symbol) {
			return "", ErrInvalidTagName
		}
	}
	return normalized, nil
}

// markStationsDirty обновляет выборку волн после rename/delete связанного тега.
func (s *TagService) markStationsDirty() {
	if s.stations != nil {
		s.stations.MarkAllDirty()
	}
}
