package cmd

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/server"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
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

func ExportUpgradedGenesisCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export-upgraded-genesis [input-genesis-file] [new_val_owner] [new_val_operator] [new_val_pubkey_json] [output-genesis-file]",
		Short: "Export upgraded genesis from a provided genesis export",
		Long: `Export upgraded genesis from a provided genesis export
Example:
	bitsongd export-upgraded-genesis bitsong_export.json bitsong1ws92lwks8xuw9wwlt0w7jrzee8raggc4u9pyrz bitsongvaloper1ws92lwks8xuw9wwlt0w7jrzee8raggc4apadnl '{"@type":"/cosmos.crypto.ed25519.PubKey","key":"5Fujs6ObfVSQPZUXlu/DkO0Lv5yjunyoGjmJzi8Sf8k="}' new-bitsong-genesis.json
`,
		Args: cobra.ExactArgs(5),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)
			serverCtx := server.GetServerContextFromCmd(cmd)
			config := serverCtx.Config
			config.SetRoot(clientCtx.HomeDir)

			genesisFile := args[0]
			newValOwner := args[1]
			newValOperator := args[2]
			newValPubKey := args[3]
			newGenesisOutput := args[4]
			genState, err := getGenStateFromPath(genesisFile)
			if err != nil {
				return err
			}

			authGenesis := authtypes.GenesisState{}
			clientCtx.JSONCodec.MustUnmarshalJSON(genState["auth"], &authGenesis)
			accounts, err := authtypes.UnpackAccounts(authGenesis.Accounts)
			if err != nil {
				panic(err)
			}
			accounts = authtypes.SanitizeGenesisAccounts(accounts)

			// add new account into auth.Accounts
			accValOwner, err := sdk.AccAddressFromBech32(newValOwner)
			if err != nil {
				return err
			}
			accounts = append(accounts, authtypes.NewBaseAccount(accValOwner, nil, 0, 0))

			packedAccs, err := authtypes.PackAccounts(accounts)
			if err != nil {
				return err
			}
			authGenesis.Accounts = packedAccs
			authGenesisBz := clientCtx.JSONCodec.MustMarshalJSON(&authGenesis)
			genState["auth"] = authGenesisBz

			// add balances object into bank.Balances
			bankGenesis := banktypes.GenesisState{}
			clientCtx.JSONCodec.MustUnmarshalJSON(genState["bank"], &bankGenesis)
			bankGenesis.Balances = append(bankGenesis.Balances, banktypes.Balance{
				Address: newValOwner,
				Coins:   sdk.NewCoins(sdk.NewInt64Coin("ubtsg", 1000_000_000)),
			})
			bankGenesisBz := clientCtx.JSONCodec.MustMarshalJSON(&bankGenesis)
			genState["bank"] = bankGenesisBz

			// TODO: think of removing genutil.GenTxs
			// TODO: try starting the chain with new genesis

			// Produce the map of address to total atom balance, both staked and UnbondingStake
			snapshotAccs := make(map[string]DerivedAccount)

			stakingGenesis := stakingtypes.GenesisState{}
			clientCtx.JSONCodec.MustUnmarshalJSON(genState["staking"], &stakingGenesis)

			var pubKey cryptotypes.PubKey
			if err := clientCtx.Codec.UnmarshalInterfaceJSON([]byte(newValPubKey), &pubKey); err != nil {
				panic(err)
			}

			var pkAny *codectypes.Any
			if pubKey != nil {
				var err error
				if pkAny, err = codectypes.NewAnyWithValue(pubKey); err != nil {
					panic(err)
				}
			}

			stakingGenesis.Validators = []stakingtypes.Validator{{
				OperatorAddress:   newValOperator,
				ConsensusPubkey:   pkAny,
				Jailed:            false,
				Status:            stakingtypes.Bonded,
				Tokens:            sdk.NewInt(100000000),
				DelegatorShares:   sdk.NewDec(100000000),
				Description:       stakingtypes.NewDescription("moniker1", "", "", "", ""),
				UnbondingHeight:   0,
				UnbondingTime:     time.Time{},
				Commission:        types.NewCommission(sdk.NewDecWithPrec(1, 2), sdk.NewDecWithPrec(10, 2), sdk.NewDecWithPrec(1, 2)),
				MinSelfDelegation: sdk.NewInt(1),
			}}

			stakingGenesis.Delegations = []stakingtypes.Delegation{{
				DelegatorAddress: newValOwner,
				ValidatorAddress: newValOperator,
				Shares:           sdk.NewDec(100000000),
			}}

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
			f, err := os.Create(newGenesisOutput)
			defer f.Close()

			if err != nil {
				return fmt.Errorf("failed to open file: %w", err)
			}

			return err
		},
	}

	return cmd
}
