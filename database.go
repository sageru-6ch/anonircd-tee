package main

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"
)

const DATABASE_VERSION = 1

const (
	PERMISSION_SUPERADMIN = 1
	PERMISSION_ADMIN      = 2
	PERMISSION_MODERATOR  = 3
	PERMISSION_VIP        = 4
)

var tables = map[string][]string{
	"meta": {
		"`key` TEXT NULL PRIMARY KEY",
		"`value` TEXT NULL"},
	"accounts": {
		"`id` INTEGER PRIMARY KEY AUTOINCREMENT",
		"`username` TEXT NULL",
		"`password` TEXT NULL"},
	"channels": {
		"`key` TEXT PRIMARY KEY",
		"`topic` TEXT NULL",
		"`password` TEXT NULL"},
	"permissions": {
		"`account` INTEGER NULL",
		"`channel` TEXT NULL",
		"`permission` INTEGER NULL"},
	"bans": {
		"`channel` TEXT NULL",
		"`type` INTEGER NULL",
		"`target` TEXT NULL",
		"`expires` INTEGER NULL",
		"`reason` TEXT NULL"}}

type DBAccount struct {
	ID       int
	Username string
}

type DBChannel struct {
	Key   int
	Topic string
}

type DBPermission struct {
	Account    int
	Channel    string
	Permission int
}

type DBBan struct {
	Channel string
	Type    int
	Target  string
	Expires int
	Reason  string
}

type Database struct {
	db *sql.DB
}

func (d *Database) Connect(driver string, dataSource string) error {
	var err error
	d.db, err = sql.Open(driver, dataSource)
	if err != nil {
		return errors.Wrapf(err, "failed to connect to %s database", driver)
	}

	err = d.CreateTables()
	if err != nil {
		return errors.Wrap(err, "failed to create tables")
	}

	err = d.Migrate()
	if err != nil {
		return errors.Wrap(err, "failed to migrate database")
	}

	return err
}

func (d *Database) CreateTables() error {
	for tname, tcolumns := range tables {
		_, err := d.db.Exec(fmt.Sprintf("CREATE TABLE IF NOT EXISTS `%s` (%s)", tname, strings.Join(tcolumns, ",")))
		if err != nil {
			return errors.Wrapf(err, "failed to create %s table", tname)
		}
	}

	return nil
}

func (d *Database) Migrate() error {
	rows, err := d.db.Query("SELECT `value` FROM meta WHERE `key`=? LIMIT 1", "version")
	if err != nil {
		return errors.Wrap(err, "failed to fetch database version")
	}

	version := 0
	for rows.Next() {
		v := ""
		err = rows.Scan(&v)
		if err != nil {
			return errors.Wrap(err, "failed to fetch database version")
		}

		version, err = strconv.Atoi(v)
		if err != nil {
			version = -1
		}
	}

	if version == -1 {
		panic("Unable to migrate database: database version unknown")
	} else if version == 0 {
		_, err := d.db.Exec("UPDATE meta SET `value`=? WHERE `key`=?", strconv.Itoa(DATABASE_VERSION), "version")
		if err != nil {
			return errors.Wrap(err, "failed to save database version")
		}
	} else if version < DATABASE_VERSION {
		// DATABASE_VERSION 2 migration queries will go here
	}

	return nil
}

func (d *Database) Close() error {
	err := d.db.Close()
	if err != nil {
		err = errors.Wrap(err, "failed to close database")
	}
	return err
}

// Accounts

func (d *Database) Account(id int) (*DBAccount, error) {
	rows, err := d.db.Query("SELECT id, username FROM accounts WHERE id=? LIMIT 1", id)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch account")
	}

	var a *DBAccount
	for rows.Next() {
		a = new(DBAccount)
		err = rows.Scan(&a.ID, a.Username)
		if err != nil {
			return nil, errors.Wrap(err, "failed to fetch account")
		}
	}

	return a, nil
}

// TODO: Lockout on too many failed attempts
func (d *Database) Auth(username string, password string) (int, error) {
	rows, err := d.db.Query("SELECT id FROM accounts")
	if err != nil {
		return 0, errors.Wrap(err, "failed to authenticate account")
	}

	accountid := 0
	for rows.Next() {
		err = rows.Scan(&accountid)
		if err != nil {
			return 0, errors.Wrap(err, "failed to authenticate account")
		}
	}

	return accountid, nil
}

func (d *Database) AddAccount(username string, hashedPassword string) error {
	_, err := d.db.Exec("INSERT INTO accounts (username, password) VALUES (?, ?)", username, hashedPassword)
	if err != nil {
		err = errors.Wrap(err, "failed to add account")
	}

	return err
}

// Channels
