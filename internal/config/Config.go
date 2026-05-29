package config

import "github.com/spf13/viper"

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
	Dir string `mapstructure:"dir"`
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

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
