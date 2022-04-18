package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"sort"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/spf13/cobra"
	tmjson "github.com/tendermint/tendermint/libs/json"
	tmtypes "github.com/tendermint/tendermint/types"
)

type DeriveSnapshotStaked struct {
	NumberAccounts uint64          `json:"num_accounts"`
	Accounts       []StakedAccount `json:"accounts"`
}

type StakedAccount struct {
	Address  string  `json:"address"`
	Staked   sdk.Int `json:"staked"`
	UsdValue sdk.Int `json:"usd_value"`
}

type AssetInfo struct {
	denom   string
	price   sdk.Dec
	decimal int64
}

// DerivedAccount provide fields of snapshot per account
type DerivedAccount struct {
	Address string  `json:"address"`
	Staked  sdk.Int `json:"staked"`
}

func newDerivedAccount(address string) DerivedAccount {
	return DerivedAccount{
		Address: address,
		Staked:  sdk.ZeroInt(),
	}
}

func getGenStateFromPath(genesisFilePath string) (map[string]json.RawMessage, error) {
	genState := make(map[string]json.RawMessage)

	genesisFile, err := os.Open(genesisFilePath)
	if err != nil {
		return genState, err
	}
	defer genesisFile.Close()

	byteValue, _ := ioutil.ReadAll(genesisFile)

	var doc tmtypes.GenesisDoc
	err = tmjson.Unmarshal(byteValue, &doc)
	if err != nil {
		return genState, err
	}

	err = json.Unmarshal(doc.AppState, &genState)
	if err != nil {
		panic(err)
	}
	return genState, nil
}

func ExportStakedBalancesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export-staked-balances [input-genesis-file] [staked-snapshot-csv]",
		Short: "Export staked balances from a provided genesis export",
		Long: `Export staked balances from a provided genesis export
Example:
	bitsongd export-staked-balances bitsong_export.json btsg-staked-snapshot.csv
`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)
			serverCtx := server.GetServerContextFromCmd(cmd)
			config := serverCtx.Config
			config.SetRoot(clientCtx.HomeDir)

			genesisFile := args[0]
			genState, err := getGenStateFromPath(genesisFile)
			if err != nil {
				return err
			}
			stakeSnapshotOutput := args[1]

			authGenesis := authtypes.GenesisState{}
			clientCtx.JSONCodec.MustUnmarshalJSON(genState["auth"], &authGenesis)
			accounts, err := authtypes.UnpackAccounts(authGenesis.Accounts)
			if err != nil {
				panic(err)
			}
			accounts = authtypes.SanitizeGenesisAccounts(accounts)

			// Produce the map of address to total atom balance, both staked and UnbondingStake
			snapshotAccs := make(map[string]DerivedAccount)

			stakingGenesis := stakingtypes.GenesisState{}
			clientCtx.JSONCodec.MustUnmarshalJSON(genState["staking"], &stakingGenesis)

			// Make a map from validator operator address to the v036 validator type
			validators := make(map[string]stakingtypes.Validator)
			for _, validator := range stakingGenesis.Validators {
				validators[validator.OperatorAddress] = validator
			}

			for _, delegation := range stakingGenesis.Delegations {
				address := delegation.DelegatorAddress

				acc, ok := snapshotAccs[address]
				if !ok {
					acc = newDerivedAccount(address)
				}

				val := validators[delegation.ValidatorAddress]
				stakedOsmos := delegation.Shares.MulInt(val.Tokens).Quo(val.DelegatorShares).RoundInt()

				acc.Staked = acc.Staked.Add(stakedOsmos)

				snapshotAccs[address] = acc
			}

			assetInfo := map[string]AssetInfo{
				"ubtsg": {
					denom:   "btsg",
					decimal: 6,
					price:   sdk.NewDecWithPrec(1251, 4), // 0.1251
				},
			}
			_ = assetInfo

			stakedAccs := []StakedAccount{}
			for addr, account := range snapshotAccs {
				if account.Staked.GTE(sdk.NewInt(400_000_000)) { // >= 400 btsg
					info := assetInfo["ubtsg"]
					decimalPow := sdk.NewDec(10).Power(uint64(info.decimal))
					usdValue := info.price.Mul(account.Staked.ToDec()).Quo(decimalPow)
					stakedAccs = append(stakedAccs, StakedAccount{
						Address:  addr,
						Staked:   account.Staked,
						UsdValue: usdValue.RoundInt(),
					})
				}
			}

			sort.SliceStable(stakedAccs, func(i, j int) bool {
				return stakedAccs[i].Staked.GT(stakedAccs[j].Staked)
			})

			fmt.Printf("# accounts: staked=%d,\n", len(stakedAccs))

			// export snapshot json
			f, err := os.Create(stakeSnapshotOutput)
			defer f.Close()

			if err != nil {
				return fmt.Errorf("failed to open file: %w", err)
			}

			var records = [][]string{{"address", "staked_value", "usd_value"}}
			for _, acc := range stakedAccs {
				records = append(records, []string{
					acc.Address, acc.Staked.String(), acc.UsdValue.String(),
				})
			}

			w := csv.NewWriter(f)
			err = w.WriteAll(records)
			return err
		},
	}

	return cmd
}
