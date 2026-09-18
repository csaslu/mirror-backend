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
	Web struct {
		// Dir is the directory holding the built frontend, served at "/".
		// Relative paths are resolved against the working directory.
		Dir string `mapstructure:"dir"`
	} `mapstructure:"web"`
	Mirror struct {
		// Proxy enables serving mirror content and directory listings through
		// the caching proxy at /{key}/...
		Proxy bool `mapstructure:"proxy"`

		// CacheAddr is host:port of the httpcached instance.
		CacheAddr string `mapstructure:"cache_addr"`

		// CacheHost is the Host header used when talking to the cache, which
		// must appear in its `sites[].hosts` list. Defaults to CacheAddr.
		CacheHost string `mapstructure:"cache_host"`

		// CacheScheme is http or https.
		CacheScheme string `mapstructure:"cache_scheme"`

		// CacheDir is where the caching proxy stores its blobs. Put it on the
		// disk that holds the artefacts.
		CacheDir string `mapstructure:"cache_dir"`

		// CacheMaxSize is the proxy's soft disk cap (e.g. "200GB").
		CacheMaxSize string `mapstructure:"cache_max_size"`

		// CacheConfig is the path of the caching proxy's configuration file.
		// Setting it makes -dump-cache-config default to that path, and makes
		// startup warn when the file is missing. It is never created
		// automatically: a mistyped path should be visible, not silently
		// materialised.
		CacheConfig string `mapstructure:"cache_config"`

		// Host is the public hostname of this mirror site, used when building
		// redirect locations that point back at us.
		Host string `mapstructure:"host"`

		// CORSAllowOrigins lists browser origins allowed to call the API from
		// another host, which is the case when the frontend runs on the Nuxt
		// dev server. Same-origin deployments need nothing here; never use "*"
		// for a site that serves credentials.
		CORSAllowOrigins []string `mapstructure:"cors_allow_origins"`
	} `mapstructure:"mirror"`
}
