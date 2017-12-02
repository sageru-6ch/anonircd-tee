package main

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/gorilla/securecookie"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"
)

const DATABASE_VERSION = 1

var ErrAccountExists = errors.New("account exists")
var ErrChannelExists = errors.New("channel exists")

var tables = map[string][]string{
	"meta": {
		"`key` TEXT NULL PRIMARY KEY",
		"`value` TEXT NULL"},
	"accounts": {
		"`id` INTEGER PRIMARY KEY AUTOINCREMENT",
		"`username` TEXT NULL",
		"`password` TEXT NULL"},
	"channels": {
		"`channel` TEXT PRIMARY KEY",
		"`topic` TEXT NULL",
		"`topictime` INTEGER NULL",
		"`password` TEXT NULL"},
	"permissions": {
		"`channel` TEXT NULL",
		"`account` INTEGER NULL",
		"`permission` INTEGER NULL"},
	"bans": {
		"`channel` TEXT NULL",
		"`type` INTEGER NULL",
		"`target` TEXT NULL",
		"`expires` INTEGER NULL",
		"`reason` TEXT NULL"}}

type DBAccount struct {
	ID         int
	Username   string
	Permission int
}

type DBChannel struct {
	Channel   string
	Topic     string
	TopicTime int
	Password  string
}

type DBPermission struct {
	Channel    string
	Account    int
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

	err = d.Initialize()
	if err != nil {
		return errors.Wrap(err, "failed to initialize database")
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

func (d *Database) Initialize() error {
	username := ""
	err := d.db.QueryRow("SELECT username FROM accounts").Scan(&username)
	if err == sql.ErrNoRows {
		err := d.AddAccount("admin", "password")
		if err != nil {
			return errors.Wrap(err, "failed to create first account")
		}

		ac := &DBChannel{Channel: "&", Topic: "Secret Area of VIP Quality"}
		d.AddChannel(1, ac)

		uc := &DBChannel{Channel: "#", Topic: "Welcome to AnonIRC"}
		d.AddChannel(1, uc)
	} else if err != nil {
		return errors.Wrap(err, "failed to check for first account")
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
	rows, err := d.db.Query("SELECT id, username FROM accounts WHERE id=?", id)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch account")
	}

	var a *DBAccount
	for rows.Next() {
		a = new(DBAccount)
		err = rows.Scan(&a.ID, &a.Username)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan account")
		}
	}

	return a, nil
}

func (d *Database) AccountU(username string) (*DBAccount, error) {
	rows, err := d.db.Query("SELECT id, username FROM accounts WHERE username=?", generateHash(username))
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch account by username")
	}

	var a *DBAccount
	for rows.Next() {
		a = new(DBAccount)
		err = rows.Scan(&a.ID, &a.Username)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan account")
		}
	}

	return a, nil
}

// TODO: Lockout on too many failed attempts
func (d *Database) Auth(username string, password string) (int, error) {
	// TODO: Salt in config
	rows, err := d.db.Query("SELECT id FROM accounts WHERE username=? AND password=?", generateHash(username), generateHash(username+"-"+password))
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

func (d *Database) GenerateToken() string {
	return base64.URLEncoding.EncodeToString(securecookie.GenerateRandomKey(64))
}

func (d *Database) AddAccount(username string, password string) error {
	ex, err := d.AccountU(username)
	if err != nil {
		return errors.Wrap(err, "failed to search for existing account while adding account")
	} else if ex != nil {
		return ErrAccountExists
	}

	_, err = d.db.Exec("INSERT INTO accounts (username, password) VALUES (?, ?)", generateHash(username), generateHash(username+"-"+password))
	if err != nil {
		return errors.Wrap(err, "failed to add account")
	}

	return nil
}

func (d *Database) SetUsername(accountid int, username string, password string) error {
	ex, err := d.AccountU(username)
	if err != nil {
		return errors.Wrap(err, "failed to search for existing account while setting username")
	} else if ex != nil {
		return ErrAccountExists
	}

	_, err = d.db.Exec("UPDATE accounts SET username=?, password=? WHERE id=?", generateHash(username), generateHash(username+"-"+password), accountid)
	if err != nil {
		return errors.Wrap(err, "failed to set username")
	}

	return nil
}

func (d *Database) SetPassword(accountid int, username string, password string) error {
	_, err := d.db.Exec("UPDATE accounts SET password=? WHERE id=?", generateHash(username+"-"+password), accountid)
	if err != nil {
		return errors.Wrap(err, "failed to set password")
	}

	return nil
}

// Channels

func (d *Database) ChannelID(id int) (*DBChannel, error) {
	rows, err := d.db.Query("SELECT channel, topic FROM channels WHERE id=?", id)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch channel")
	}

	var c *DBChannel
	for rows.Next() {
		c = new(DBChannel)
		err = rows.Scan(&c.Channel, &c.Topic)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan channel")
		}
	}

	return c, nil
}

func (d *Database) Channel(channel string) (*DBChannel, error) {
	rows, err := d.db.Query("SELECT channel, topic FROM channels WHERE channel=?", generateHash(channel))
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch channel by key")
	}

	var c *DBChannel
	for rows.Next() {
		c = new(DBChannel)
		err = rows.Scan(&c.Channel, &c.Topic)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan channel")
		}
	}

	return c, nil
}

func (d *Database) AddChannel(accountid int, channel *DBChannel) error {
	ex, err := d.Channel(channel.Channel)
	if err != nil {
		return errors.Wrap(err, "failed to search for existing channel while adding channel")
	} else if ex != nil {
		return ErrChannelExists
	}

	chch := channel.Channel
	channel.Channel = generateHash(strings.ToLower(channel.Channel))
	_, err = d.db.Exec("INSERT INTO channels (channel, topic, topictime, password) VALUES (?, ?, ?, ?)", channel.Channel, channel.Topic, channel.TopicTime, channel.Password)
	if err != nil {
		return errors.Wrap(err, "failed to add channel")
	}

	err = d.SetPermission(accountid, chch, PERMISSION_SUPERADMIN)
	if err != nil {
		return errors.Wrap(err, "failed to set permission on newly added channel")
	}

	return nil
}
func (d *Database) GetPermission(accountid int, channel string) (int, error) {
	rows, err := d.db.Query("SELECT permission FROM permissions WHERE account=? AND channel=?", accountid, generateHash(channel))
	if err != nil {
		return 0, errors.Wrap(err, "failed to authenticate account")
	}

	permission := PERMISSION_USER
	for rows.Next() {
		err = rows.Scan(&permission)
		if err != nil {
			return 0, errors.Wrap(err, "failed to authenticate account")
		}
	}

	return permission, nil
}

func (d *Database) SetPermission(accountid int, channel string, permission int) error {
	acc, err := d.Account(accountid)
	if err != nil {
		panic(err)
	} else if acc == nil {
		return nil
	}

	ch, err := d.Channel(channel)
	if err != nil {
		return errors.Wrap(err, "failed to fetch channel while setting permission")
	} else if ch == nil {
		return nil
	}
	chh := generateHash(channel)

	rows, err := d.db.Query("SELECT permission FROM permissions WHERE account=? AND channel=?", accountid, chh)
	if err != nil {
		return errors.Wrap(err, "failed to set permission")
	}

	if !rows.Next() {
		_, err = d.db.Exec("INSERT INTO permissions (channel, account, permission) VALUES (?, ?, ?)", chh, accountid, permission)
		if err != nil {
			return errors.Wrap(err, "failed to set permission")
		}
	} else {
		_, err = d.db.Exec("UPDATE permissions SET permission=? WHERE account=? AND channel=?", permission, accountid, chh)
		if err != nil {
			return errors.Wrap(err, "failed to set permission")
		}
	}

	return nil
}
