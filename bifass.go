package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gopkg.in/yaml.v2"
)

const STATS_PERIOD = 1
const IDLE_THREAD_SLEEP_MS = 2000
const RUNNING_THREAD_SLEEP_MS = 10

var cfg Config
var active_threads int
var started bool
var reset_pending bool

var AccountNames []string
var AccountCurrentBalance []int
var AccountCurrentFee []int

var transaction_counter []int
var failure_counter []int
var insufficient_balance_counter []int
var last_total_transactions = 0

var total_balance_by_lua = 0

var total_transactions = 0
var transactions_per_second = 0
var total_failure = 0
var total_insufficient_balance = 0

func loadConfig() {
	fmt.Println("Parsing config.yaml")
	f, err := os.Open("config.yaml")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(&cfg)
	if err != nil {
		panic(err)
	}

	active_threads = 1
	started = false
	reset_pending = false

	for i := range cfg.Accounts {
		AccountNames = append(AccountNames, cfg.Accounts[i].Name)
		AccountCurrentBalance = append(AccountCurrentBalance, cfg.Accounts[i].Balance)
		AccountCurrentFee = append(AccountCurrentFee, 0)
	}

	transaction_counter = make([]int, cfg.Options.ThreadsMax)
	failure_counter = make([]int, cfg.Options.ThreadsMax)
	insufficient_balance_counter = make([]int, cfg.Options.ThreadsMax)

	rand.Seed(time.Now().Unix())
}

func loadFunctions(ctx context.Context, rdb *redis.Client) {
	// load balance transfer function
	fmt.Println("Loading balance transfer function")
	dat, err := os.ReadFile("balance_transfer.lua")
	if err != nil {
		panic(err)
	}
	rdb.Do(ctx, "function", "load", "replace", string(dat))

	// load total balance function
	fmt.Println("Loading get total balance function")
	dat, err = os.ReadFile("get_total_balance.lua")
	if err != nil {
		panic(err)
	}
	rdb.Do(ctx, "function", "load", "replace", string(dat))
}

func loadAccounts(ctx context.Context, rdb *redis.Client) {
	for i := range cfg.Accounts {
		fmt.Println("Setting balance for account "+cfg.Accounts[i].Name+" to "+strconv.Itoa(cfg.Accounts[i].Balance), ", fee to 0")
		err := rdb.Set(ctx, "balance:"+cfg.Accounts[i].Name, cfg.Accounts[i].Balance, 0).Err()
		if err != nil {
			panic(err)
		}
		err = rdb.Set(ctx, "fee:"+cfg.Accounts[i].Name, 0, 0).Err()
		if err != nil {
			panic(err)
		}
	}
}

func updateStats(ctx context.Context, rdb *redis.Client) {
	// get account balances individually
	for i := range cfg.Accounts {
		name := cfg.Accounts[i].Name
		val, err := rdb.Get(ctx, "balance:"+name).Result()
		if err != nil {
			val = err.Error()
		}
		AccountCurrentBalance[i], _ = strconv.Atoi(val)
		val, err = rdb.Get(ctx, "fee:"+name).Result()
		if err != nil {
			val = err.Error()
		}
		AccountCurrentFee[i], _ = strconv.Atoi(val)
		// fmt.Println("  "+name+" balance: ", cfg.Accounts[i].CurrentBalance, " fee: ", cfg.Accounts[i].CurrentFee)
	}

	// get total balance by lua script
	// build the args as slices to the function
	var args []interface{}
	args = append(args, "fcall")
	args = append(args, "get_total_balance")
	args = append(args, len(cfg.Accounts)*2)
	for i := range cfg.Accounts {
		args = append(args, "balance:"+cfg.Accounts[i].Name)
		args = append(args, "fee:"+cfg.Accounts[i].Name)
	}

	val, err := rdb.Do(ctx, args...).Result()
	if err != nil {
		val = err.Error()
	}

	total_balance_by_lua, _ = strconv.Atoi(fmt.Sprint(val))
	// fmt.Println("TOTAL balance+fee using function: ", strconv.Itoa(total_balance_by_lua))

	// counters and stats
	total_transactions = 0
	total_failure = 0
	total_insufficient_balance = 0

	// print statistics
	for i := 0; i < cfg.Options.ThreadsMax; i++ {
		total_transactions += transaction_counter[i]
		total_failure += failure_counter[i]
		total_insufficient_balance += insufficient_balance_counter[i]
	}

	transactions_per_second = 0
	transactions_per_second = (total_transactions - last_total_transactions) / STATS_PERIOD
	last_total_transactions = total_transactions

	// fmt.Println("Total transactions: ", total_transactions, ", TPS: "+strconv.Itoa(transactions_per_second))
	// fmt.Println("Total failures: ", total_failure)
	// fmt.Println("Total rejected insufficient balance: ", total_insufficient_balance)
}

func balanceTransfer(num int) {
	var ctx = context.Background()
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.Db,
	})

	failure_counter[num] = 0
	insufficient_balance_counter[num] = 0

	for {
		if (!started) || (num >= active_threads) {
			time.Sleep(time.Millisecond * IDLE_THREAD_SLEEP_MS)
			continue
		} else {
			time.Sleep(time.Millisecond * RUNNING_THREAD_SLEEP_MS)
		}

		// get random source and target account
		sourceAcc := cfg.Accounts[rand.Intn(len(cfg.Accounts))].Name
		targetAcc := cfg.Accounts[rand.Intn(len(cfg.Accounts))].Name

		// making sure that transfer targets a different account
		for sourceAcc == targetAcc {
			targetAcc = cfg.Accounts[rand.Intn(len(cfg.Accounts))].Name
		}

		// randomize transfer account over 0
		var amount = 0
		for amount == 0 {
			amount = rand.Intn(cfg.Options.TransferMax)
		}

		val, err := rdb.Do(ctx, "fcall", "balance_transfer", "3", "balance:"+sourceAcc, "balance:"+targetAcc, "fee:"+sourceAcc, amount,
			cfg.Options.Fee).Result()

		transaction_counter[num]++
		if err != nil {
			// technical failures
			failure_counter[num]++
		} else if val == int64(-1) { // !#$*U!#)#*!g comparison
			// insufficient balance failure
			insufficient_balance_counter[num]++
		}
	}
}

func invokeHandler(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/invoke/")

	fmt.Println("Invoked: " + id)

	if strings.HasPrefix(id, "thread/") {
		count := strings.TrimPrefix(id, "thread/")
		active_threads, _ = strconv.Atoi(count)
	} else {
		switch id {
		case "go":
			started = true
		case "pause":
			started = false
		case "reset":
			reset_pending = true
		}
	}

	fmt.Println("thread: " + strconv.Itoa(active_threads) + ", started: " + strconv.FormatBool(started))
	w.Write([]byte(id))
}

func eventsHandler(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers to allow all origins
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Type")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// loop update events
	for {
		update := UpdateEvent{
			TotalTransactions:                total_transactions,
			TransactionsPerSecond:            transactions_per_second,
			TotalFailures:                    total_failure,
			TotalRejectedInsufficientBalance: total_insufficient_balance,
			AccountNames:                     AccountNames,
			AccountCurrentBalance:            AccountCurrentBalance,
			AccountCurrentFee:                AccountCurrentFee,
			TotalBalanceByLua:                total_balance_by_lua,
		}

		p, _ := json.Marshal(update)
		fmt.Fprintf(w, "data: %s\n\n", p)
		w.(http.Flusher).Flush()

		time.Sleep(time.Second * STATS_PERIOD) // STATS_PERIOD seconds wait -- this is so golang
	}

	// Simulate closing the connection
	closeNotify := w.(http.CloseNotifier).CloseNotify()
	<-closeNotify
}

func main() {
	// load configuration YAML
	loadConfig()

	var ctx = context.Background()
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.Db,
	})

	// load Redis functions
	loadFunctions(ctx, rdb)

	// initialize initial account balances from config
	loadAccounts(ctx, rdb)

	fmt.Println("Initialization complete")

	// update initial statistics
	updateStats(ctx, rdb)

	// start webserver GUI
	http.Handle("/", http.FileServer(http.Dir("./static")))
	http.HandleFunc("/invoke/", invokeHandler)
	http.HandleFunc("/events", eventsHandler)
	go func() { http.ListenAndServe(":8080", nil) }()

	// execute balance transfer tasks as much as max threads
	for i := 0; i < cfg.Options.ThreadsMax; i++ {
		go balanceTransfer(i)
	}

	// loop update statistics
	for {
		updateStats(ctx, rdb)
		time.Sleep(time.Second * STATS_PERIOD) // STATS_PERIOD seconds wait
		if reset_pending {                     // reset accounts
			reset_pending = false
			started = false
			time.Sleep(time.Second * STATS_PERIOD)
			loadAccounts(ctx, rdb)
		}
	}
}
