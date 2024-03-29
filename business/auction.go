package business

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/mock"
	"log"
	"math/big"
	"os"
	"strings"
)

type AuctionStatus struct {
	Ended      bool  `json:"ended"`
	HighestBid int64 `json:"highestBid"`
}

type Stats struct {
	Bids   int64 `json:"bids"`
	Volume int64 `json:"volume"`
}

type Bid struct {
	Sender string
	Amount int64
}

type EthConnection struct {
	client *ethclient.Client
}

type EthConnectionMock struct {
	mock.Mock
}

type HigherBidAlreadySubmitted struct{}
type AuctionEnded struct{}

type Connection interface {
	GetAuctionStatus() (AuctionStatus, error)
	ListAllBids() ([]Bid, error)
	Bid(amount int64) error
	Stats() (Stats, error)
	Deploy(durationInSeconds int64, beneficiaryAddress string) (string, string, error)
}

func (err HigherBidAlreadySubmitted) Error() string {

	return "There already is a higher bid"
}

func (err AuctionEnded) Error() string {

	return "Auction already ended"
}

func NewBlockchainConnection() (*EthConnection, error) {

	instanceUrl, found := os.LookupEnv("INSTANCE_URL")
	if !found {
		return nil, errors.New("Instance url is needed to connect to an ethereum node")
	}

	client, err := ethclient.Dial(instanceUrl)
	if err != nil {
		return nil, err
	}

	return &EthConnection{
		client: client,
	}, nil
}

func (ec *EthConnection) GetAuctionStatus() (AuctionStatus, error) {

	actionStatus := AuctionStatus{}

	contractAddr, found := os.LookupEnv("CONTRACT_DEPLOYMENT_ADDR")
	if !found {
		return AuctionStatus{}, errors.New("Instance url is needed to connect to an ethereum node")
	}

	addr := common.HexToAddress(contractAddr)

	acn, err := NewSimpleAuction(addr, ec.client)
	if err != nil {
		return AuctionStatus{}, err
	}

	endTime, err := acn.AuctionEndTime(nil)
	if err != nil {
		return AuctionStatus{}, err
	}

	header, err := ec.client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		return AuctionStatus{}, err
	}

	actionStatus.Ended = header.Time > endTime.Uint64()

	highestBid, err := acn.HighestBid(nil)
	if err != nil {
		return AuctionStatus{}, err
	}

	actionStatus.HighestBid = highestBid.Int64()

	return actionStatus, nil
}

func (ec *EthConnection) ListAllBids() ([]Bid, error) {

	contractAddr, found := os.LookupEnv("CONTRACT_DEPLOYMENT_ADDR")
	if !found {
		return nil, errors.New("contract address is needed to connect to the auction")
	}

	addr := common.HexToAddress(contractAddr)

	query := ethereum.FilterQuery{
		Addresses: []common.Address{
			addr,
		},
	}

	logs, err := ec.client.FilterLogs(context.Background(), query)
	if err != nil {
		return nil, err
	}

	contractAbi, err := abi.JSON(strings.NewReader(SimpleAuctionMetaData.ABI))
	if err != nil {
		return nil, err
	}

	var bids []Bid

	for _, log := range logs {

		event, err := contractAbi.Unpack("HighestBidIncreased", log.Data)
		if err != nil {
			return nil, err
		}

		bid := Bid{
			Sender: event[0].(common.Address).String(),
			Amount: event[1].(*big.Int).Int64(),
		}

		bids = append(bids, bid)
	}

	return bids, nil
}

func (ec *EthConnection) Bid(amount int64) error {

	contractAddr, found := os.LookupEnv("CONTRACT_DEPLOYMENT_ADDR")
	if !found {
		return errors.New("contract address is needed to connect to the auction")
	}

	addr := common.HexToAddress(contractAddr)

	acn, err := NewSimpleAuction(addr, ec.client)
	if err != nil {
		return err
	}

	bidderKey, found := os.LookupEnv("BIDDER_ACC_PRIVATE_KEY")
	if !found {
		return errors.New("bidder private is required to submit a bid to the auction")
	}

	ks := keystore.NewKeyStore("./keys", keystore.StandardScryptN, keystore.StandardScryptP)

	privateKeyBytes, err := hex.DecodeString(bidderKey)
	if err != nil {
		log.Println(err, "----1")
		return err
	}

	privateKey, err := crypto.ToECDSA(privateKeyBytes)
	if err != nil {
		log.Println(err, "----2")
		return err
	}

	// no need to add passphrases since the key is deleted right away
	acc, err := ks.ImportECDSA(privateKey, "")
	if err != nil {
		log.Println(err, "----3")
		return err
	}

	key, err := ks.Export(acc, "", "")
	if err != nil {
		log.Println(err, "----4")
		return err
	}

	err = ks.Delete(acc, "")
	if err != nil {
		log.Println(err, "----5")
		return err
	}

	chainId, err := ec.client.ChainID(context.Background())
	if err != nil {
		log.Println(err, "----6")
		return err
	}

	auth, err := bind.NewTransactorWithChainID(bytes.NewReader(key), "", chainId)
	if err != nil {
		log.Printf("Failed to create authorized transactor: %v", err)
		return err
	}

	auth.Value = big.NewInt(amount)

	_, err = acn.Bid(auth)
	if err != nil {

		if strings.Contains(err.Error(), "There already is a higher bid") {
			return HigherBidAlreadySubmitted{}
		}

		if strings.Contains(err.Error(), "Auction already ended") {
			return AuctionEnded{}
		}

		return err
	}

	return nil
}

func (ec *EthConnection) Stats() (Stats, error) {

	contractAddr, found := os.LookupEnv("CONTRACT_DEPLOYMENT_ADDR")
	if !found {
		return Stats{}, errors.New("contract address is needed to connect to the auction")
	}

	addr := common.HexToAddress(contractAddr)

	query := ethereum.FilterQuery{
		Addresses: []common.Address{
			addr,
		},
	}

	logs, err := ec.client.FilterLogs(context.Background(), query)
	if err != nil {
		return Stats{}, err
	}

	contractAbi, err := abi.JSON(strings.NewReader(SimpleAuctionMetaData.ABI))
	if err != nil {
		return Stats{}, err
	}

	var stats Stats

	for _, eventLog := range logs {

		event, err := contractAbi.Unpack("HighestBidIncreased", eventLog.Data)
		if err != nil {
			return Stats{}, err
		}

		stats.Bids += 1
		stats.Volume += event[1].(*big.Int).Int64()
	}

	return stats, nil
}

func (ec *EthConnection) Deploy(durationInSeconds int64, beneficiaryAddress string) (string, string, error) {

	deployerKey, found := os.LookupEnv("DEPLOYER_ACC_PRIVATE_KEY")
	if !found {
		return "", "", errors.New("deployer private is required to deploy the auction contract")
	}

	ks := keystore.NewKeyStore("./keys", keystore.StandardScryptN, keystore.StandardScryptP)

	privateKeyBytes, err := hex.DecodeString(deployerKey)
	if err != nil {
		log.Println(err)
		return "", "", err
	}

	privateKey, err := crypto.ToECDSA(privateKeyBytes)
	if err != nil {
		log.Println(err)
		return "", "", err
	}

	acc, err := ks.ImportECDSA(privateKey, "")
	if err != nil {
		log.Println(err)
		return "", "", err
	}

	key, err := ks.Export(acc, "", "")
	if err != nil {
		log.Println(err)
		return "", "", err
	}

	err = ks.Delete(acc, "")
	if err != nil {
		log.Println(err)
		return "", "", err
	}

	chainId, err := ec.client.ChainID(context.Background())
	if err != nil {
		log.Println(err)
		return "", "", err
	}

	auth, err := bind.NewTransactorWithChainID(bytes.NewReader(key), "", chainId)
	if err != nil {
		log.Printf("Failed to create authorized transactor: %v", err)
		return "", "", err
	}

	beneficiary := common.HexToAddress(beneficiaryAddress)

	address, tx, _, err := DeploySimpleAuction(auth, ec.client, big.NewInt(durationInSeconds), beneficiary)
	if err != nil {
		log.Printf("Failed to deploy new auction contract: %v", err)
		return "", "", err
	}

	return address.String(), tx.Hash().String(), nil
}

func (m *EthConnectionMock) GetAuctionStatus() (AuctionStatus, error) {

	args := m.Called()
	return args.Get(0).(AuctionStatus), args.Error(1)
}

func (m *EthConnectionMock) ListAllBids() ([]Bid, error) {

	args := m.Called()
	return args.Get(0).([]Bid), args.Error(1)
}

func (m *EthConnectionMock) Bid(amount int64) error {

	args := m.Called(amount)
	return args.Error(0)
}

func (m *EthConnectionMock) Stats() (Stats, error) {

	args := m.Called()
	return args.Get(0).(Stats), args.Error(1)
}

func (m *EthConnectionMock) Deploy(durationInSeconds int64, beneficiaryAddress string) (string, string, error) {

	args := m.Called(durationInSeconds, beneficiaryAddress)
	return args.Get(0).(string), args.Get(1).(string), args.Error(2)
}
