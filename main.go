package main

import (
	"context"
	"fmt"
	"opml-opt/db"
	"opml-opt/llamago"
	"opml-opt/log"
	"opml-opt/mips"
	"opml-opt/mips/vm"
	"opml-opt/rpc"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	cli "gopkg.in/urfave/cli.v1"
	yaml "gopkg.in/yaml.v2"
)

var (
	OriginCommandHelpTemplate = `{{.Name}}{{if .Subcommands}} command{{end}}{{if .Flags}} [command options]{{end}} {{.ArgsUsage}}
{{if .Description}}{{.Description}}
{{end}}{{if .Subcommands}}
SUBCOMMANDS:
  {{range .Subcommands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
  {{end}}{{end}}{{if .Flags}}
OPTIONS:
{{range $.Flags}}   {{.}}
{{end}}
{{end}}`
)
var app *cli.App

var (
	configPathFlag = cli.StringFlag{
		Name:  "config",
		Usage: "config path",
		Value: "./config.yml",
	}
	logLevelFlag = cli.IntFlag{
		Name:  "log",
		Usage: "log level",
		Value: log.InfoLog,
	}
	logFilePath = cli.StringFlag{
		Name:  "logPath",
		Usage: "log root path",
		Value: "./logs",
	}
	promptFlag = cli.StringFlag{
		Name:  "prompt",
		Value: "Why Golang is so popular?",
	}
)

func init() {
	app = cli.NewApp()
	app.Version = "v1.0.0"
	app.Flags = []cli.Flag{
		configPathFlag,
		logLevelFlag,
		logFilePath,
	}
	app.Action = Start
	app.Commands = []cli.Command{commandMips}
	cli.CommandHelpTemplate = OriginCommandHelpTemplate
}

func main() {
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type Config struct {
	Port          string `yaml:"port"`
	Host          string `yaml:"host"`
	ModelName     string `yaml:"model_name"`
	ModelPath     string `yaml:"model_path"`
	MipsProgram   string `yaml:"mips_program"`
	MongoURI      string `yaml:"mongo_uri"`
	DispatcherUrl string `yaml:"dispatcher"`
}

var commandMips = cli.Command{
	Name:  "mips",
	Usage: "run mips",
	Flags: []cli.Flag{
		configPathFlag,
		promptFlag,
	},
	Action: RunMips,
}

func RunMips(ctx *cli.Context) {
	conf := loadConfig(ctx)
	prompt := ctx.String(promptFlag.Name)
	vm.ModelPath = conf.ModelPath
	vm.MIPS_PROGRAM = conf.MipsProgram
	nodeHash, err := vm.RunCheckPointZeroRoot(prompt)
	if err != nil {
		panic(err)
	}
	println("ok:", nodeHash.String())
}

func Start(ctx *cli.Context) {
	defer func() {
		db.MgoCli.Disconnect(context.Background())
	}()
	logLevel := ctx.Int(logLevelFlag.Name)
	fmt.Println("log level", logLevel)
	logPath := ctx.String(logFilePath.Name)

	filename := fmt.Sprintf("/operator_%v.log", strings.ReplaceAll(time.Now().Format("2006-01-02 15:04:05"), " ", "_"))
	fmt.Println("log file path", logPath+filename)
	err := os.MkdirAll(logPath, 0777)
	if err != nil {
		panic(err)
	}
	logFile, err := os.Create(logPath + filename)
	if err != nil {
		panic(err)
	}
	defer logFile.Close()

	log.InitLog(log.DebugLog, logFile)

	conf := loadConfig(ctx)
	if conf.Host != "" {
		rpc.Host = conf.Host
	}
	//init db
	db.MongoURI = conf.MongoURI
	// db.Init()

	//init workers
	err = llamago.InitWorker(conf.ModelName, conf.ModelPath)
	if err != nil {
		log.Fatal(err)
	}
	err = mips.InitWorker(conf.ModelName, conf.ModelPath, conf.MipsProgram)
	if err != nil {
		log.Fatal(err)
	}

	rpc.InitRpcService(conf.Port, conf.ModelName, conf.ModelPath)

	go callHeartBeat(conf)

	contx := context.Background()
	err = rpc.RpcServer.Start(contx)
	if err != nil {
		log.Fatal(err)
	}
	waitToExit()
}

func loadConfig(ctx *cli.Context) Config {
	var optConfig Config
	if ctx.IsSet(configPathFlag.Name) {
		configPath := ctx.String(configPathFlag.Name)
		b, err := os.ReadFile(configPath)
		if err != nil {
			log.Fatal("read config error", err)
		}
		mips.ConfigPath = configPath
		err = yaml.Unmarshal(b, &optConfig)
		if err != nil {
			log.Fatal(err)
		}
	}
	return optConfig
}

func waitToExit() {
	exit := make(chan bool, 0)
	sc := make(chan os.Signal, 1)
	if !signal.Ignored(syscall.SIGHUP) {
		signal.Notify(sc, syscall.SIGHUP)
	}
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for sig := range sc {
			fmt.Printf("received exit signal:%v", sig.String())
			close(exit)
			break
		}
	}()
	<-exit
}
