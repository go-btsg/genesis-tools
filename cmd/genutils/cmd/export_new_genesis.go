package cmd

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/server"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/spf13/cobra"
	"github.com/tendermint/tendermint/crypto/secp256k1"
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

func getGenStateFromPath(genesisFilePath string) (tmtypes.GenesisDoc, map[string]json.RawMessage, error) {
	genState := make(map[string]json.RawMessage)
	var doc tmtypes.GenesisDoc

	genesisFile, err := os.Open(genesisFilePath)
	if err != nil {
		return doc, genState, err
	}
	defer genesisFile.Close()

	byteValue, _ := ioutil.ReadAll(genesisFile)

	err = tmjson.Unmarshal(byteValue, &doc)
	if err != nil {
		return doc, genState, err
	}

	err = json.Unmarshal(doc.AppState, &genState)
	if err != nil {
		panic(err)
	}
	return doc, genState, nil
}

func writeGenStateToPath(doc tmtypes.GenesisDoc, path string, genState map[string]json.RawMessage) error {
	f, err := os.Create(path)
	defer f.Close()

	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}

	genStateBz, err := json.MarshalIndent(genState, "", " ")
	if err != nil {
		return err
	}
	doc.AppState = genStateBz

	byteValue, err := tmjson.Marshal(&doc)
	if err != nil {
		return err
	}

	ioutil.WriteFile(path, byteValue, 0644)
	return nil
}

func ExportUpgradedGenesisCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export-upgraded-genesis [input-genesis-file] [new_val_owner] [new_val_operator] [new_val_pubkey_json] [output-genesis-file]",
		Short: "Export upgraded genesis from a provided genesis export",
		Long: `Export upgraded genesis from a provided genesis export
Example:
	genutils export-upgraded-genesis bitsong_export.json bitsong13m350fvnk3s6y5n8ugxhmka277r0t7cw48ru47 bitsongvaloper13m350fvnk3s6y5n8ugxhmka277r0t7cw5rl49r '{"@type":"/cosmos.crypto.ed25519.PubKey","key":"Dst4aT7mWIUriAO5IrGAxMoLh+ratiG92DHCOSZ8rAo="}' new-bitsong-genesis.json
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
			doc, genState, err := getGenStateFromPath(genesisFile)
			if err != nil {
				return err
			}

			authGenesis := authtypes.GenesisState{}
			clientCtx.Codec.MustUnmarshalJSON(genState["auth"], &authGenesis)
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
			authGenesisBz := clientCtx.Codec.MustMarshalJSON(&authGenesis)
			genState["auth"] = authGenesisBz

			// add balances object into bank.Balances
			bankGenesis := banktypes.GenesisState{}
			clientCtx.JSONCodec.MustUnmarshalJSON(genState["bank"], &bankGenesis)
			bankCoins := sdk.NewCoins(sdk.NewInt64Coin("ubtsg", 1000_000_000))
			var bondedCoins sdk.Coins
			var notBondedCoins sdk.Coins
			bankGenesis.Supply = bankGenesis.Supply.Add(bankCoins...)
			bankGenesis.Balances = append(bankGenesis.Balances, banktypes.Balance{
				Address: newValOwner,
				Coins:   bankCoins,
			})

			bondedPoolAddr := authtypes.NewModuleAddress(stakingtypes.BondedPoolName)
			notBondedPoolAddr := authtypes.NewModuleAddress(stakingtypes.NotBondedPoolName)
			fmt.Println("bondedPoolAddr", bondedPoolAddr.String())
			for _, balance := range bankGenesis.Balances {
				if balance.Address == bondedPoolAddr.String() {
					fmt.Println("bondedPoolAddr-match-found", bondedPoolAddr.String(), balance.Coins.String())
					bondedCoins = balance.Coins
				}
				if balance.Address == notBondedPoolAddr.String() {
					fmt.Println("notBondedPoolAddr-match-found", notBondedPoolAddr.String(), balance.Coins.String())
					notBondedCoins = balance.Coins
				}
			}

			bankGenesisBz := clientCtx.JSONCodec.MustMarshalJSON(&bankGenesis)
			genState["bank"] = bankGenesisBz

			// TODO: think of removing genutil.GenTxs
			// TODO: try starting the chain with new genesis

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

			valPriv2 := secp256k1.GenPrivKey().PubKey()
			valOper2 := sdk.ValAddress(valPriv2.Address())
			var pk2Any *codectypes.Any
			if pk2Any, err = codectypes.NewAnyWithValue(pubKey); err != nil {
				panic(err)
			}
			fmt.Println("unbondedValidator", valOper2.String())

			stakingGenesis.Exported = false
			stakingGenesis.Validators = []stakingtypes.Validator{{
				OperatorAddress:   newValOperator,
				ConsensusPubkey:   pkAny,
				Jailed:            false,
				Status:            stakingtypes.Bonded,
				Tokens:            bondedCoins.AmountOf("ubtsg"),
				DelegatorShares:   bondedCoins.AmountOf("ubtsg").ToDec(),
				Description:       stakingtypes.NewDescription("moniker1", "", "", "", ""),
				UnbondingHeight:   0,
				UnbondingTime:     time.Time{},
				Commission:        types.NewCommission(sdk.NewDecWithPrec(1, 2), sdk.NewDecWithPrec(10, 2), sdk.NewDecWithPrec(1, 2)),
				MinSelfDelegation: sdk.NewInt(1),
			}, {
				OperatorAddress:   valOper2.String(),
				ConsensusPubkey:   pk2Any,
				Jailed:            false,
				Status:            stakingtypes.Unbonded,
				Tokens:            notBondedCoins.AmountOf("ubtsg"),
				DelegatorShares:   notBondedCoins.AmountOf("ubtsg").ToDec(),
				Description:       stakingtypes.NewDescription("moniker2", "", "", "", ""),
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
			stakingGenesis.UnbondingDelegations = []stakingtypes.UnbondingDelegation{}

			stakingGenesisBz := clientCtx.JSONCodec.MustMarshalJSON(&stakingGenesis)
			genState["staking"] = stakingGenesisBz

			// update distribution genesis
			distrGenesis := distrtypes.GenesisState{}
			clientCtx.JSONCodec.MustUnmarshalJSON(genState["distribution"], &distrGenesis)

			distrGenesis.DelegatorStartingInfos = []distrtypes.DelegatorStartingInfoRecord{
				{
					DelegatorAddress: newValOwner,
					ValidatorAddress: newValOperator,
					StartingInfo: distrtypes.DelegatorStartingInfo{
						Height:         0,
						PreviousPeriod: 1,
						Stake:          bondedCoins.AmountOf("ubtsg").ToDec(),
					},
				},
				{
					DelegatorAddress: sdk.AccAddress(valOper2).String(),
					ValidatorAddress: valOper2.String(),
					StartingInfo: distrtypes.DelegatorStartingInfo{
						Height:         0,
						PreviousPeriod: 1,
						Stake:          notBondedCoins.AmountOf("ubtsg").ToDec(),
					},
				},
			}
			distrGenesis.DelegatorWithdrawInfos = []distrtypes.DelegatorWithdrawInfo{}
			distrGenesis.OutstandingRewards = []distrtypes.ValidatorOutstandingRewardsRecord{}
			distrGenesis.PreviousProposer = sdk.ConsAddress(pubKey.Address()).String()
			distrGenesis.ValidatorAccumulatedCommissions = []distrtypes.ValidatorAccumulatedCommissionRecord{}
			distrGenesis.ValidatorCurrentRewards = []distrtypes.ValidatorCurrentRewardsRecord{}
			distrGenesis.ValidatorHistoricalRewards = []distrtypes.ValidatorHistoricalRewardsRecord{}
			distrGenesis.ValidatorSlashEvents = []distrtypes.ValidatorSlashEventRecord{}

			distrGenesisBz := clientCtx.JSONCodec.MustMarshalJSON(&distrGenesis)
			genState["distribution"] = distrGenesisBz

			// 			genState["distribution"] = []byte(fmt.Sprintf(`{
			//   "delegator_starting_infos": [
			//     {
			//       "delegator_address": "%s",
			//       "starting_info": {
			//         "height": "0",
			//         "previous_period": "1",
			//         "stake": "100000000.000000000000000000"
			//       },
			//       "validator_address": "%s"
			//     }
			//   ],
			//   "delegator_withdraw_infos": {},
			//   "fee_pool": {
			//     "community_pool": [
			//       {
			//         "amount": "110908.340000000000000000",
			//         "denom": "ubtsg"
			//       }
			//     ]
			//   },
			//   "outstanding_rewards": [
			//     {
			//       "outstanding_rewards": [
			//         {
			//           "amount": "5434508.660000000000000000",
			//           "denom": "ubtsg"
			//         }
			//       ],
			//       "validator_address": "%s"
			//     }
			//   ],
			//   "params": {
			//     "base_proposer_reward": "0.010000000000000000",
			//     "bonus_proposer_reward": "0.040000000000000000",
			//     "community_tax": "0.020000000000000000",
			//     "withdraw_addr_enabled": true
			//   },
			//   "previous_proposer": "%s",
			//   "validator_accumulated_commissions": [
			//     {
			//       "accumulated": {
			//         "commission": [
			//           {
			//             "amount": "543450.866000000000000000",
			//             "denom": "ubtsg"
			//           }
			//         ]
			//       },
			//       "validator_address": "%s"
			//     }
			//   ],
			//   "validator_current_rewards": [
			//     {
			//       "rewards": {
			//         "period": "2",
			//         "rewards": [
			//           {
			//             "amount": "4891057.794000000000000000",
			//             "denom": "ubtsg"
			//           }
			//         ]
			//       },
			//       "validator_address": "%s"
			//     }
			//   ],
			//   "validator_historical_rewards": [
			//     {
			//       "period": "1",
			//       "rewards": {
			//         "cumulative_reward_ratio": {},
			//         "reference_count": 2
			//       },
			//       "validator_address": "%s"
			//     }
			//   ],
			//   "validator_slash_events": {}
			// }`, newValOwner, newValOperator, newValOperator, sdk.ConsAddress(pubKey.Address()).String(), newValOperator,
			// 				newValOperator, newValOperator))

			// export snapshot json
			writeGenStateToPath(doc, newGenesisOutput, genState)

			return err
		},
	}

	return cmd
}
