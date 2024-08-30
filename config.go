package main

// YAML configuration structure
type Config struct {
	Redis struct {
		Addr     string `yaml:"addr"`
		Password string `yaml:"password"`
		Db       int    `yaml:"db"`
	} `yaml:"redis"`
	Options struct {
		ThreadsMax  int `yaml:"threads_max"`
		TransferMax int `yaml:"transfer_max"`
		Fee         int `yaml:"fee"`
	} `yaml:"options"`
	Accounts []struct {
		Name    string `yaml:"name"`
		Balance int    `yaml:"balance"`
	} `yaml:"accounts"`
}
