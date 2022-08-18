/*---------------------------------------------------------------------------------------------
 *  Copyright (c) IBAX All rights reserved.
 *  See LICENSE in the project root for license information.
 *--------------------------------------------------------------------------------------------*/

package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/IBAX-io/go-ibax/packages/consts"
	log "github.com/sirupsen/logrus"

	"github.com/IBAX-io/go-explorer/conf"

	"github.com/IBAX-io/go-ibax/packages/storage/sqldb"

	"gorm.io/gorm"
)

type DbTransaction struct {
	conn *gorm.DB
}
type DayBlock struct {
	Id     int64 `gorm:"not null"`
	Tx     int32 `gorm:"not null"`
	Length int64 `gorm:"not null"`
}

func isFound(db *gorm.DB) (bool, error) {
	if errors.Is(db.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return true, db.Error
}

func InitDatabase() error {
	DatabaseInfo := conf.GetEnvConf().DatabaseInfo
	if err := DatabaseInfo.GormInit(); err != nil {
		log.WithFields(log.Fields{"warn": err}).Warn("gorm init failed:")
		return err
	}
	if err := NewDbTransaction(GetDB(nil)).DropTables(); err != nil {
		log.WithFields(log.Fields{"type": consts.DBError, "error": err}).Error("dropping all tables")
		return err
	}
	return nil
}

// GormClose is closing Gorm connection
func GormClose() error {
	//fmt.Println("gorm close!")
	if err := conf.GetEnvConf().DatabaseInfo.Close(); err != nil {
		return err
	}
	return nil
}

func NewDbTransaction(conn *gorm.DB) *DbTransaction {
	return &DbTransaction{conn: conn}
}

// StartTransaction is beginning transaction
func StartTransaction() (*DbTransaction, error) {
	conn := conf.GetDbConn().Conn().Begin()
	if conn.Error != nil {
		log.WithFields(log.Fields{"type": consts.DBError, "error": conn.Error}).Error("cannot start transaction because of connection error")
		return nil, conn.Error
	}
	return &DbTransaction{
		conn: conn,
	}, nil
}

// Rollback is transaction rollback
func (tr *DbTransaction) Rollback() {
	tr.conn.Rollback()
}

// Commit is transaction commit
func (tr *DbTransaction) Commit() error {
	return tr.conn.Commit().Error
}

// Connection returns connection of database
func (tr *DbTransaction) Connection() *gorm.DB {
	return tr.conn
}

// DropTables is dropping all of the tables
func (dbTx *DbTransaction) DropTables() error {
	return GetDB(dbTx).Exec(`
	DO $$ DECLARE
	    r RECORD;
	BEGIN
	    FOR r IN (SELECT tablename FROM pg_tables WHERE schemaname = current_schema()) LOOP
		EXECUTE 'DROP TABLE IF EXISTS ' || quote_ident(r.tablename) || ' CASCADE';
	    END LOOP;
	END $$;
	`).Error
}

func GetALL(tableName string, order string, v any) error {
	return conf.GetDbConn().Conn().Table(tableName).Order(order).Find(v).Error
}

func GetBlockid(hash []byte) (int64, error) {
	lt := sqldb.LogTransaction{}
	fount, err := lt.GetByHash(hash)
	if err == nil && fount {
		return lt.Block, nil
	}
	return -1, err
}

var (
	Gret []DBTransactionsInfo
)

func GetDBDealTraninfo(limit int) error {
	var (
		err error
	)
	if err = GetBlockInfoToRedis(limit); err != nil {
		log.WithFields(log.Fields{"warn": err}).Warn("GetDayBlockInfoToRedis err")
	}
	return err
}

func SendTpsListToWebsocket(dayblock []DayBlock) error {
	var ret []ScanOutBlockTransactionRet
	var err error
	for i := 0; i < len(dayblock); i++ {
		var info = ScanOutBlockTransactionRet{
			BlockId:           dayblock[i].Id,
			BlockSizes:        dayblock[i].Length,
			BlockTranscations: int64(dayblock[i].Tx),
		}
		ret = append(ret, info)
	}
	err = SendTopTransactiontps(&ret)
	if err != nil {
		return err
	}
	return nil
}

func SendTopTransactiontps(topBlockTps *[]ScanOutBlockTransactionRet) error {
	err := SendDashboardDataToWebsocket(topBlockTps, ChannelBlockTpsList)
	if err != nil {
		return err
	}
	return nil
}

func GetTraninfoFromRedis(limit int) (*[]ScanOutBlockTransactionRet, error) {
	var ret []ScanOutBlockTransactionRet
	var err error
	var transBlock []DayBlock

	rd := RedisParams{
		Key:   "block-tpslist",
		Value: "",
	}
	if err = rd.Get(); err != nil {
		log.WithFields(log.Fields{"warn": err}).Warn("GetTraninfoFromRedis getdb err")
		return nil, err
	}
	if err = json.Unmarshal([]byte(rd.Value), &transBlock); err != nil {
		log.WithFields(log.Fields{"warn": err}).Warn("GetTraninfoFromRedis json err")
		return nil, err
	}

	for i := 0; i < len(transBlock); i++ {
		var info = ScanOutBlockTransactionRet{
			BlockId:           transBlock[i].Id,
			BlockSizes:        transBlock[i].Length,
			BlockTranscations: int64(transBlock[i].Tx),
		}
		ret = append(ret, info)
	}
	return &ret, err
}

func GetBlockInfoToRedis(limit int) error {
	var trans []DayBlock
	if err := GetDB(nil).Raw(`SELECT block_chain."id",LENGTH(block_chain."data"),block_chain.tx FROM block_chain ORDER BY id desc LIMIT 30`).Find(&trans).Error; err != nil {
		return err
	}
	value, err := json.Marshal(trans)
	if err != nil {
		return err
	}
	rd := RedisParams{
		Key:   "block-tpslist",
		Value: string(value),
	}
	if err := rd.Set(); err != nil {
		return err
	}
	if err := SendTpsListToWebsocket(trans); err != nil {
		return fmt.Errorf("SendTpsListToWebsocket err:%s", err.Error())
	}
	return nil
}

func GetDayblockinfoFromRedis(t1, t2 int64, transBlock []Block) (int32, error) {
	var (
		dat int32
		err error
	)

	dlen := len(transBlock)
	dat = 0
	for i := 0; i < dlen; i++ {
		if transBlock[i].Time > t1 && transBlock[i].Time < t2 {
			dat += transBlock[i].Tx
		}
	}
	return dat, err
}

func GetDBDayTraninfo(day int) (*[]DBTransactionsInfo, error) {
	return &Gret, nil
}

func HasTableOrView(tr *DbTransaction, names string) bool {
	var name string
	conf.GetDbConn().Conn().Table("information_schema.tables").
		Where("table_type IN ('BASE TABLE', 'VIEW') AND table_schema NOT IN ('pg_catalog', 'information_schema') AND table_name=?", names).
		Select("table_name").Row().Scan(&name)

	return name == names
}

//HasTable p is struct Pointer
func HasTable(p any) bool {
	if !GetDB(nil).Migrator().HasTable(p) {
		return false
	}
	return true
}