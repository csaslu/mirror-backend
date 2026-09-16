package config

type StaticConfig struct {
	Server struct {
		Host string `mapstructure:"host"`
		Port int    `mapstructure:"port"`
	} `mapstructure:"server"`
	Log struct {
		File struct {
			All string `mapstructure:"all"`
			Err string `mapstructure:"err"`
		} `mapstructure:"file"`
		MaxSize    int  `mapstructure:"max_size"`
		MaxBackups int  `mapstructure:"max_backups"`
		MaxAge     int  `mapstructure:"max_age"`
		Compress   bool `mapstructure:"compress"`
	} `mapstructure:"log"`
	Database struct {
		Host string `mapstructure:"host"`
		Port int    `mapstructure:"port"`
		User string `mapstructure:"user"`
		Name string `mapstructure:"name"`
		Pass string `mapstructure:"pass"`
	} `mapstructure:"database"`
	Cache struct {
		Host string `mapstructure:"host"`
		Port int    `mapstructure:"port"`
		Pass string `mapstructure:"pass"`
		DB   int    `mapstructure:"db"`
	} `mapstructure:"cache"`
}
