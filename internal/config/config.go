package config

import (
	"strings"

	"github.com/spf13/viper"
)

// Config 存储所有应用程序的配置
// Config stores all configuration for the application
type Config struct {
	DatabaseURL string `mapstructure:"DATABASE_URL"`
	GoFiBaseDir string `mapstructure:"GOFI_BASE_DIR"`
	GoFiPort    string `mapstructure:"GOFI_PORT"`
	GinMode     string `mapstructure:"GIN_MODE"`
}

// LoadConfig 从配置文件和环境变量中加载配置，configPath 为空时默认当前目录下的 config.toml
// LoadConfig loads configuration from config file and environment variables; when configPath is empty it defaults to ./config.toml
func LoadConfig(configPath string) (*Config, error) {
	v := viper.New()

	// 设置配置文件路径和名称
	// Set config file path and name
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.AddConfigPath(".")
		v.SetConfigName("config")
		v.SetConfigType("toml")
	}

	// 设置环境变量
	// Set environment variables
	v.SetEnvPrefix("GOFI")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// 设置默认值
	// Set default values
	v.SetDefault("GOFI_PORT", "8080")
	v.SetDefault("GIN_MODE", "debug")
	v.SetDefault("GOFI_BASE_DIR", "/app/data")

	// 读取配置文件
	// Read config file
	if configPath != "" {
		if err := v.ReadInConfig(); err != nil {
			return nil, err
		}
	} else {
		_ = v.ReadInConfig() // 忽略错误，因为配置文件是可选的
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
