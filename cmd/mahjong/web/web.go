package web

import (
	"context"
	"fmt"
	"net/http"

	_ "github.com/lonnng/nanoserver/cmd/mahjong/web/provider/yxsdk/payment/test"
	_ "github.com/lonnng/nanoserver/cmd/mahjong/web/provider/yxsdk/payment/wechat"
	_ "github.com/lonnng/nanoserver/cmd/mahjong/web/provider/yxsdk/payment/yx"
	"github.com/lonnng/nanoserver/cmd/mahjong/web/service/desk"
	"github.com/lonnng/nanoserver/cmd/mahjong/web/service/history"
	"github.com/lonnng/nanoserver/cmd/mahjong/web/service/login"
	"github.com/lonnng/nanoserver/cmd/mahjong/web/service/order"
	"github.com/lonnng/nanoserver/db"
	"github.com/lonnng/nanoserver/internal/algoutil"
	"github.com/lonnng/nanoserver/internal/protocol"
	"github.com/lonnng/nanoserver/internal/whitelist"

	"os"
	"os/signal"
	"syscall"

	"github.com/lonnng/nex"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type Closer func()

var logger = log.WithField("component", "http")

func dbStartup() func() {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?%s",
		viper.GetString("database.username"),
		viper.GetString("database.password"),
		viper.GetString("database.host"),
		viper.GetString("database.port"),
		viper.GetString("database.dbname"),
		viper.GetString("database.args"))

	return db.MustStartup(
		dsn,
		db.MaxIdleConns(viper.GetInt("database.max_idle_conns")),
		db.MaxIdleConns(viper.GetInt("database.max_open_conns")),
		db.ShowSQL(viper.GetBool("database.show_sql")))
}

func enableWhiteList() {
	whitelist.Setup(viper.GetStringSlice("whitelist.ip"))
}

func version() (*protocol.Version, error) {
	return &protocol.Version{
		Version: viper.GetInt("update.version"),
		Android: viper.GetString("update.android"),
		IOS:     viper.GetString("update.ios"),
	}, nil
}

func pongHandler() (string, error) {
	return "pong", nil
}

func logRequest(ctx context.Context, r *http.Request) (context.Context, error) {
	if uri := r.RequestURI; uri != "/ping" {
		logger.Debugf("Method=%s, RemoteAddr=%s URL=%s", r.Method, r.RemoteAddr, uri)
	}
	return ctx, nil
}

func startupService() http.Handler {
	var (
		ctx    = context.Background()
		mux    = http.NewServeMux()
		webDir = viper.GetString("webserver.static_dir")
		addr   = viper.GetString("webserver.addr")
	)

	println("====>", webDir)
	println("====>", addr)

	//set up services
	orderService := order.NewService(logger)
	historyService := history.NewService(logger)
	deskService := desk.NewService(logger)

	nex.Before(logRequest)

	mux.Handle("/v1/user/", login.MakeHandler())
	mux.Handle("/v1/order/", order.MakeHandler(ctx, orderService))
	mux.Handle("/v1/history/", history.MakeHandler(ctx, historyService))
	mux.Handle("/v1/desk/", desk.MakeHandler(ctx, deskService))
	mux.Handle("/v1/version", nex.Handler(version))

	// GM系统命令
	mux.Handle("/v1/gm/reset", nex.Handler(resetPlayerHandler).Before(authFilter))   // 重置玩家未完成房间状态
	mux.Handle("/v1/gm/consume", nex.Handler(cardConsumeHandler).Before(authFilter)) // 设置房卡消耗
	mux.Handle("/v1/gm/broadcast", nex.Handler(broadcast).Before(authFilter))        // 消息广播
	mux.Handle("/v1/gm/kick", nex.Handler(kickHandler).Before(authFilter))           // 踢人
	mux.Handle("/v1/gm/online", nex.Handler(onlineHandler).Before(authFilter))       // 在线信息
	mux.Handle("/v1/gm/recharge", nex.Handler(rechargeHandler).Before(authFilter))   // 玩家充值
	mux.Handle("/v1/gm/query/user/", nex.Handler(userInfoHandler))                   // 玩家信息查询

	//统计后台

	mux.Handle("/v1/stats/user/register", nex.Handler(registerUsersHandler).Before(authFilter))     // 注册人数
	mux.Handle("/v1/stats/user/activation", nex.Handler(activationUsersHandler).Before(authFilter)) // 活跃人数
	mux.Handle("/v1/stats/online", nex.Handler(onlineLiteHandler).Before(authFilter))               // 同时在线人、桌数
	mux.Handle("/v1/stats/retention", nex.Handler(retentionHandler).Before(authFilter))             // 留存
	mux.Handle("/v1/stats/rank", nex.Handler(rankHandler).Before(authFilter))                       // 分数,对局排名
	mux.Handle("/v1/stats/consume", nex.Handler(cardConsumeStatsHandler).Before(authFilter))        // 房卡消耗

	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(webDir))))
	mux.Handle("/ping", nex.Handler(pongHandler))

	return algoutil.AccessControl(algoutil.OptionControl(mux))
}

func Startup() {
	// setup database
	closer := dbStartup()
	defer closer()

	// enable white list
	enableWhiteList()

	var (
		addr      = viper.GetString("webserver.addr")
		cert      = viper.GetString("webserver.certificates.cert")
		key       = viper.GetString("webserver.certificates.key")
		enableSSL = viper.GetBool("webserver.enable_ssl")
	)

	go func() {
		// http service
		mux := startupService()
		if enableSSL {
			log.Fatal(http.ListenAndServeTLS(addr, cert, key, mux))
		} else {
			log.Fatal(http.ListenAndServe(addr, mux))
		}
	}()

	sg := make(chan os.Signal)
	signal.Notify(sg, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGKILL)
	// stop server
	select {
	case s := <-sg:
		log.Infof("got signal: %s", s.String())
	}
}
