package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/eve-network/eve/airdrop/config"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func cosmos() ([]banktypes.Balance, []config.Reward, int, error) {
	delegators := []stakingtypes.DelegationResponse{}

	rpc := config.GetCosmosHubConfig().API + "/cosmos/staking/v1beta1/validators?pagination.limit=" + strconv.Itoa(LimitPerPage) + "&pagination.count_total=true"
	validatorsResponse, err := fetchValidatorsWithRetry(rpc)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("failed to fetch validator for Cosmos: %w", err)
	}
	validators := validatorsResponse.Validators
	fmt.Println("Validators: ", len(validators))
	for validatorIndex, validator := range validators {
		url := config.GetCosmosHubConfig().API + "/cosmos/staking/v1beta1/validators/" + validator.OperatorAddress + "/delegations?pagination.limit=" + strconv.Itoa(LimitPerPage) + "&pagination.count_total=true"
		delegations, total, err := fetchDelegationsWithRetry(url)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("failed to fetch Delegations for Cosmos: %w", err)
		}
		fmt.Println(validator.OperatorAddress)
		fmt.Println("Response ", len(delegations))
		fmt.Println("Cosmos validator "+strconv.Itoa(validatorIndex)+" ", total)
		delegators = append(delegators, delegations...)
	}

	usd := math.LegacyMustNewDecFromStr("20")

	apiURL := APICoingecko + config.GetCosmosHubConfig().CoinID + "&vs_currencies=usd"
	tokenInUsd, err := fetchCosmosTokenPriceWithRetry(apiURL)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("failed to fetch Cosmos token price: %w", err)
	}
	tokenIn20Usd := usd.QuoTruncate(tokenInUsd)

	rewardInfo := []config.Reward{}
	balanceInfo := []banktypes.Balance{}

	totalTokenDelegate := math.LegacyMustNewDecFromStr("0")
	for _, delegator := range delegators {
		validatorIndex := findValidatorInfoCustomType(validators, delegator.Delegation.ValidatorAddress)
		validatorInfo := validators[validatorIndex]
		token := (delegator.Delegation.Shares.MulInt(validatorInfo.Tokens)).QuoTruncate(validatorInfo.DelegatorShares)
		if token.LT(tokenIn20Usd) {
			continue
		}
		totalTokenDelegate = totalTokenDelegate.Add(token)
	}
	eveAirdrop := math.LegacyMustNewDecFromStr(EveAirdrop)
	testAmount, _ := math.LegacyNewDecFromStr("0")
	for _, delegator := range delegators {
		validatorIndex := findValidatorInfoCustomType(validators, delegator.Delegation.ValidatorAddress)
		validatorInfo := validators[validatorIndex]
		token := (delegator.Delegation.Shares.MulInt(validatorInfo.Tokens)).QuoTruncate(validatorInfo.DelegatorShares)
		if token.LT(tokenIn20Usd) {
			continue
		}
		eveAirdrop := (eveAirdrop.MulInt64(int64(config.GetCosmosHubConfig().Percent))).QuoInt64(100).Mul(token).QuoTruncate(totalTokenDelegate)
		eveBech32Address := convertBech32Address(delegator.Delegation.DelegatorAddress)
		rewardInfo = append(rewardInfo, config.Reward{
			Address:         delegator.Delegation.DelegatorAddress,
			EveAddress:      eveBech32Address,
			Shares:          delegator.Delegation.Shares,
			Token:           token,
			EveAirdropToken: eveAirdrop,
			ChainID:         config.GetCosmosHubConfig().ChainID,
		})
		testAmount = eveAirdrop.Add(testAmount)
		balanceInfo = append(balanceInfo, banktypes.Balance{
			Address: eveBech32Address,
			Coins:   sdk.NewCoins(sdk.NewCoin("eve", eveAirdrop.TruncateInt())),
		})
	}
	fmt.Println("Cosmos ", testAmount)
	// Write delegations to file
	// fileForDebug, _ := json.MarshalIndent(rewardInfo, "", " ")
	// _ = os.WriteFile("rewards.json", fileForDebug, 0644)

	// fileBalance, _ := json.MarshalIndent(balanceInfo, "", " ")
	// _ = os.WriteFile("balance.json", fileBalance, 0644)
	return balanceInfo, rewardInfo, len(balanceInfo), nil
}

func fetchCosmosTokenPriceWithRetry(apiURL string) (math.LegacyDec, error) {
	var data math.LegacyDec
	var err error
	for attempt := 1; attempt <= MaxRetries; attempt++ {
		data, err = fetchCosmosTokenPrice(apiURL)
		if err == nil {
			return data, nil
		}
		fmt.Printf("error fetching Cosmos token price (attempt %d/%d): %v\n", attempt, MaxRetries, err)
		time.Sleep(time.Duration(time.Duration(attempt * Backoff).Milliseconds()))
	}
	return math.LegacyDec{}, fmt.Errorf("failed to fetch Cosmos token price after %d attempts", MaxRetries)
}

func fetchCosmosTokenPrice(apiURL string) (math.LegacyDec, error) {
	// Make a GET request to the API
	response, err := http.Get(apiURL)
	if err != nil {
		return math.LegacyDec{}, fmt.Errorf("error making GET request to fetch Cosmos token price: %w", err)
	}
	defer response.Body.Close()

	// Read the response body
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return math.LegacyDec{}, fmt.Errorf("error reading response body for Cosmos token price: %w", err)
	}

	var data config.CosmosPrice

	// Unmarshal the JSON byte slice into the defined struct
	err = json.Unmarshal(responseBody, &data)
	if err != nil {
		return math.LegacyDec{}, fmt.Errorf("error unmarshalling JSON for Cosmos token price: %w", err)
	}

	tokenInUsd := math.LegacyMustNewDecFromStr(data.Token.USD.String())
	return tokenInUsd, nil
}