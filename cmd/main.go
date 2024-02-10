package main

import (
	"Zookeeper/internal/zookeeper"
	"github.com/spf13/viper"
)

func init() {
	viper.SetConfigName("config")
	viper.AddConfigPath("./config")

	err := viper.ReadInConfig()
	if err != nil {
		panic(err)
	}
}

func main() {
	z := zookeeper.NewZookeeper()
	z.Run()
}
