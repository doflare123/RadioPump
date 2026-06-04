package config

import "github.com/spf13/viper"

const defaultMaxMusicFileSizeMB = 20

type Config struct {
	Server ServerConfig `mapstructure:"server"`
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
	viper.SetConfigFile("config.yaml")
	viper.SetDefault("music.dir", "./music")
	viper.SetDefault("music.max_file_size_mb", defaultMaxMusicFileSizeMB)

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
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
