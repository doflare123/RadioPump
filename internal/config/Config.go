package config

import (
	"github.com/spf13/viper"
)

type Config struct {
	Server ServerConfig `mapstructure:"server"`
	Music  MusicConfig  `mapstructure:"music"`
	Waves  []WaveConfig `mapstructure:"waves"`
	Stream StreamConfig `mapstructure:"stream"` // не слайс, это объект
}

type ServerConfig struct {
	Port          string `mapstructure:"port"`
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
	Bitrate        string `mapstructure:"bitrate"`
	Sample_rate    int    `mapstructure:"sample_rate"`
	Buffer_seconds int    `mapstructure:"buffer_seconds"`
}

func NewConfig() (*Config, error) {
	viper.SetConfigFile("config.yaml")
	viper.AddConfigPath("../")

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}
	return &config, nil
}
