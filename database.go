package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/gorilla/securecookie"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"
)

const DATABASE_VERSION = 1

var ErrAccountExists = errors.New("account already exists")
var ErrChannelExists = errors.New("channel already exists")
var ErrChannelDoesNotExist = errors.New("channel does not exist")

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

const (
	BAN_TYPE_ADDRESS = 1
	BAN_TYPE_ACCOUNT = 2
)

type DBAccount struct {
	ID       int
	Username string
	Password string
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

type DBMode struct {
	Channel string
	Mode    string
	Value   string
}

type DBBan struct {
	Channel string
	Type    int
	Target  string
	Expires int
	Reason  string
}

type Database struct {
	db *sqlx.DB
}

func (d *Database) Connect(driver string, dataSource string) error {
	var err error
	d.db, err = sqlx.Connect(driver, dataSource)
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
	if p(err) {
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
		log.Panic("Unable to migrate database: database version unknown")
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
	a, err := db.Account(1)
	if err != nil {
		return errors.Wrap(err, "failed to initialize")
	}

	if a.ID > 0 {
		return nil // Admin account exists
	}

	err = d.AddAccount("admin", "password")
	if err != nil {
		return errors.Wrap(err, "failed to create initial administrator account")
	}

	ac := &DBChannel{Channel: CHANNEL_SERVER, Topic: "Secret Area of VIP Quality"}
	d.AddChannel(1, ac)

	uc := &DBChannel{Channel: CHANNEL_LOBBY, Topic: "Welcome to AnonIRC"}
	d.AddChannel(1, uc)

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

func (d *Database) Account(id int) (DBAccount, error) {
	a := DBAccount{}
	err := d.db.Get(&a, "SELECT * FROM accounts WHERE id=? LIMIT 1", id)
	if p(err) {
		return a, errors.Wrap(err, "failed to fetch account")
	}

	return a, nil
}

func (d *Database) AccountU(username string) (DBAccount, error) {
	a := DBAccount{}
	err := d.db.Get(&a, "SELECT * FROM accounts WHERE username=? LIMIT 1", generateHash(username))
	if p(err) {
		return a, errors.Wrap(err, "failed to fetch account by username")
	}

	return a, nil
}

// TODO: Lockout on too many failed attempts
func (d *Database) Auth(username string, password string) (int, error) {
	// TODO: Salt in config
	a := DBAccount{}
	err := d.db.Get(&a, "SELECT * FROM accounts WHERE username=? AND password=? LIMIT 1", generateHash(username), generateHash(username+"-"+password))
	if p(err) {
		return 0, errors.Wrap(err, "failed to authenticate account")
	}

	return a.ID, nil
}

func (d *Database) GenerateToken() string {
	return base64.URLEncoding.EncodeToString(securecookie.GenerateRandomKey(64))
}

func (d *Database) AddAccount(username string, password string) error {
	ex, err := d.AccountU(username)
	if err != nil {
		return errors.Wrap(err, "failed to search for existing account while adding account")
	} else if ex.ID > 0 {
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
	} else if ex.ID > 0 {
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

func (d *Database) ChannelID(id int) (DBChannel, error) {
	c := DBChannel{}
	err := d.db.Get(&c, "SELECT * FROM channels WHERE id=? LIMIT 1", id)
	if p(err) {
		return c, errors.Wrap(err, "failed to fetch channel")
	}

	return c, nil
}

func (d *Database) Channel(channel string) (DBChannel, error) {
	c := DBChannel{}
	err := d.db.Get(&c, "SELECT * FROM channels WHERE channel=? LIMIT 1", generateHash(channel))
	if p(err) {
		return c, errors.Wrap(err, "failed to fetch channel by key")
	}

	return c, nil
}

func (d *Database) AddChannel(accountid int, channel *DBChannel) error {
	ex, err := d.Channel(channel.Channel)
	if err != nil {
		return errors.Wrap(err, "failed to search for existing channel while adding channel")
	} else if ex.Channel != "" {
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

// Permissions

func (d *Database) GetPermission(accountid int, channel string) (DBPermission, error) {
	dbp := DBPermission{}

	// Return REGISTERED by default
	dbp.Permission = PERMISSION_REGISTERED

	err := d.db.Get(&dbp, "SELECT * FROM permissions WHERE account=? AND channel=? LIMIT 1", accountid, generateHash(channel))
	if p(err) {
		return dbp, errors.Wrap(err, "failed to fetch permission")
	}

	return dbp, nil
}

func (d *Database) SetPermission(accountid int, channel string, permission int) error {
	acc, err := d.Account(accountid)
	if err != nil {
		log.Panicf("%+v", err)
	} else if acc.ID == 0 {
		return nil
	}

	ch, err := d.Channel(channel)
	if err != nil {
		return errors.Wrap(err, "failed to fetch channel while setting permission")
	} else if ch.Channel == "" {
		return nil
	}
	chh := generateHash(channel)

	dbp, err := d.GetPermission(accountid, chh)
	if err != nil {
		return errors.Wrap(err, "failed to set permission")
	}

	if dbp.Channel != "" {
		_, err = d.db.Exec("UPDATE permissions SET permission=? WHERE account=? AND channel=?", permission, accountid, chh)
		if err != nil {
			return errors.Wrap(err, "failed to set permission")
		}
	} else {
		_, err = d.db.Exec("INSERT INTO permissions (channel, account, permission) VALUES (?, ?, ?)", chh, accountid, permission)
		if err != nil {
			return errors.Wrap(err, "failed to set permission")
		}
	}

	return nil
}

// Bans

func (d *Database) Ban(banid int) (DBBan, error) {
	b := DBBan{}
	err := d.db.Get(&b, "SELECT * FROM bans WHERE id=? LIMIT 1", banid)
	if p(err) {
		return b, errors.Wrap(err, "failed to fetch ban")
	}

	return b, nil
}

func (d *Database) BanAddr(addrhash string, channel string) (DBBan, error) {
	b := DBBan{}
	err := d.db.Get(&b, "SELECT * FROM bans WHERE channel=? AND `type`=? AND target=?", generateHash(channel), BAN_TYPE_ADDRESS, addrhash)
	if p(err) {
		return b, errors.Wrap(err, "failed to fetch ban")
	}

	return b, nil
}

func (d *Database) BanAccount(accountid int, channel string) (DBBan, error) {
	b := DBBan{}
	err := d.db.Get(&b, "SELECT * FROM bans WHERE channel=? AND `type`=? AND target=?", generateHash(channel), BAN_TYPE_ACCOUNT, accountid)
	if p(err) {
		return b, errors.Wrap(err, "failed to fetch ban")
	}

	return b, nil
}

func (d *Database) AddBan(b DBBan) error {
	var err error

	// Channel-specific (not server-wide)
	if b.Channel != "&" {
		ex, err := d.Channel(b.Channel)
		if err != nil {
			return errors.Wrap(err, "failed to search for existing ban while adding ban")
		} else if ex.Channel == "" {
			return ErrChannelDoesNotExist
		}
	}

	_, err = d.db.Exec("INSERT INTO bans (`channel`, `type`, `target`, `expires`, `reason`) VALUES (?, ?, ?, ?, ?, ?)", b.Channel, b.Type, b.Target, b.Expires, b.Reason)
	if p(err) {
		return errors.Wrap(err, "failed to add ban")
	}

	return nil
}
