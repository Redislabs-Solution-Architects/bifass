package main

type UpdateEvent struct {
	TotalTransactions                int      `json:"TotalTransactions"`
	TransactionsPerSecond            int      `json:"TransactionsPerSecond"`
	TotalFailures                    int      `json:"TotalFailures"`
	TotalRejectedInsufficientBalance int      `json:"TotalRejectedInsufficientBalance"`
	AccountNames                     []string `json:"AccountNames"`
	AccountCurrentBalance            []int    `json:"AccountCurrentBalance"`
	AccountCurrentFee                []int    `json:"AccountCurrentFee"`
	TotalBalanceByLua                int      `json:"TotalBalanceByLua"`
}
