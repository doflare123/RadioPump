package config

import (
	"RadioPump/internal/security"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/viper"
)

const defaultMaxMusicFileSizeMB = 20

type Config struct {
	Server ServerConfig `mapstructure:"server"`
	CORS   CORSConfig   `mapstructure:"cors"`
	Music  MusicConfig  `mapstructure:"music"`
	Waves  []WaveConfig `mapstructure:"waves"`
	Stream StreamConfig `mapstructure:"stream"`
}

type ServerConfig struct {
	Port          int    `mapstructure:"port"`
	AdminName     string `mapstructure:"admin_name"`
	AdminPassword string `mapstructure:"admin_password"`
	JWTSecret     string `mapstructure:"jwt_secret"`
}

type MusicConfig struct {
	Dir           string `mapstructure:"dir"`
	MaxFileSizeMB int    `mapstructure:"max_file_size_mb"`
}

type CORSConfig struct {
	AllowedOrigins []string `mapstructure:"allowed_origins"`
}

type WaveConfig struct {
	Name string   `mapstructure:"name"`
	Tags []string `mapstructure:"tags"`
}

type StreamConfig struct {
	Bitrate       string `mapstructure:"bitrate"`
	SampleRate    int    `mapstructure:"sample_rate"`
	BufferSeconds int    `mapstructure:"buffer_seconds"`
}

func NewConfig() (*Config, error) {
	// Отдельный экземпляр исключает перенос настроек между загрузками и тестами.
	viper := viper.New()
	path := os.Getenv("RADIOPUMP_CONFIG")
	if path == "" {
		path = "config.yaml"
	}
	viper.SetConfigFile(path)
	viper.SetDefault("music.dir", "./music")
	viper.SetDefault("music.max_file_size_mb", defaultMaxMusicFileSizeMB)
	viper.SetDefault("cors.allowed_origins", []string{
		"http://localhost:5500",
		"http://127.0.0.1:5500",
	})

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("конфигурация недоступна или некорректна; используйте config.example.yaml и RADIOPUMP_CONFIG")
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, errors.New("некорректные типы полей конфигурации")
	}
	// Окружение и файлы секретов имеют приоритет над YAML; значения не логируются.
	for _, item := range []struct {
		name   string
		target *string
	}{
		{"RADIOPUMP_ADMIN_NAME", &cfg.Server.AdminName},
		{"RADIOPUMP_ADMIN_PASSWORD", &cfg.Server.AdminPassword},
		{"RADIOPUMP_JWT_SECRET", &cfg.Server.JWTSecret},
	} {
		if err := loadSetting(item.name, item.target); err != nil {
			return nil, err
		}
	}

	if err := cfg.Server.ValidateAuth(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// loadSetting ограничивает чтение секретов 4 KiB и допускает завершающий перевод
// строки в файле. Одновременные источники запрещены, пустое значение не скрывается YAML.
func loadSetting(name string, target *string) error {
	value, direct := os.LookupEnv(name)
	path, file := os.LookupEnv(name + "_FILE")
	if direct && file {
		return fmt.Errorf("задайте только %s или %s_FILE", name, name)
	}
	if direct {
		*target = value
	}
	if file {
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("%s_FILE: файл секрета недоступен", name)
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, 4097))
		if err != nil || len(data) > 4096 {
			return fmt.Errorf("%s_FILE: не удалось прочитать секрет до 4096 байт", name)
		}
		*target = strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	}
	return nil
}

// ValidateAuth отвергает небезопасные настройки до создания файлов и запуска
// сервисов. Ошибки указывают имена полей, но не раскрывают их значения.
func (s ServerConfig) ValidateAuth() error {
	if !security.ValidJWTSecret(s.JWTSecret) {
		return errors.New("server.jwt_secret: требуется случайный секрет не короче 32 байт без краевых пробелов")
	}
	if strings.TrimSpace(s.AdminName) == "" {
		return errors.New("server.admin_name: требуется непустой логин")
	}
	if len(strings.TrimSpace(s.AdminPassword)) < 12 {
		return errors.New("server.admin_password: требуется пароль не короче 12 байт без учёта краевых пробелов")
	}
	return nil
}

// Переводит человекочитаемый лимит из YAML в байты.
// Нулевое или отрицательное значение трактуем как дефолт, чтобы старые конфиги
// не ломали загрузку музыки после обновления проекта.
func (m MusicConfig) MaxFileSizeBytes() int64 {
	mb := m.MaxFileSizeMB
	if mb <= 0 {
		mb = defaultMaxMusicFileSizeMB
	}
	return int64(mb) * 1024 * 1024
}
