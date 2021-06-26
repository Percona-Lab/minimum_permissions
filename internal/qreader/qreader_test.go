package qreader

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Percona-Lab/minimum_permissions/internal/tester"
	tu "github.com/Percona-Lab/minimum_permissions/internal/testutils"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestReadGenlog(t *testing.T) {
	want := []*tester.TestingCase{
		{
			Database:         "",
			Query:            "/home/karl/mysql/my-5.7/bin/mysqld, Version: 5.7.22-log (MySQL Community Server (GPL)). started with:Tcp port: 12345  Unix socket: /tmp/12345/mysql_sandbox12345.sockTime                 Id Command    Argument",
			Fingerprint:      "",
			MinimumGrants:    nil,
			LastTestedGrants: nil,
			NotAllowed:       false,
			Error:            nil,
			InvalidQuery:     false,
		},
		{
			Database:         "",
			Query:            "select @@version_comment limit 1",
			Fingerprint:      "",
			MinimumGrants:    nil,
			LastTestedGrants: nil,
			NotAllowed:       false,
			Error:            nil,
			InvalidQuery:     false,
		},
		{
			Database:         "",
			Query:            "SHOW /*!40100 ENGINE*/ INNODB STATUS",
			Fingerprint:      "",
			MinimumGrants:    nil,
			LastTestedGrants: nil,
			NotAllowed:       false,
			Error:            nil,
			InvalidQuery:     false,
		},
		{
			Database:         "",
			Query:            "select @@version_comment limit 1",
			Fingerprint:      "",
			MinimumGrants:    nil,
			LastTestedGrants: nil,
			NotAllowed:       false,
			Error:            nil,
			InvalidQuery:     false,
		},
		{
			Database:         "",
			Query:            "SELECT DATABASE()",
			Fingerprint:      "",
			MinimumGrants:    nil,
			LastTestedGrants: nil,
			NotAllowed:       false,
			Error:            nil,
			InvalidQuery:     false,
		},
		{
			Database:         "",
			Query:            "/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */",
			Fingerprint:      "",
			MinimumGrants:    nil,
			LastTestedGrants: nil,
			NotAllowed:       false,
			Error:            nil,
			InvalidQuery:     false,
		},
		{
			Database:         "",
			Query:            "DROP TABLE IF EXISTS `columns_priv`",
			Fingerprint:      "",
			MinimumGrants:    nil,
			LastTestedGrants: nil,
			NotAllowed:       false,
			Error:            nil,
			InvalidQuery:     false,
		},
		{
			Database:         "",
			Query:            "/*!40101 SET @saved_cs_client     = @@character_set_client */",
			Fingerprint:      "",
			MinimumGrants:    nil,
			LastTestedGrants: nil,
			NotAllowed:       false,
			Error:            nil,
			InvalidQuery:     false,
		},
		{
			Database:         "",
			Query:            "/*!40101 SET character_set_client = utf8 */",
			Fingerprint:      "",
			MinimumGrants:    nil,
			LastTestedGrants: nil,
			NotAllowed:       false,
			Error:            nil,
			InvalidQuery:     false,
		},
		{
			Database:         "",
			Query:            "CREATE TABLE `columns_priv` (  `Host` char(60) COLLATE utf8_bin NOT NULL DEFAULT '',  `Db` char(64) COLLATE utf8_bin NOT NULL DEFAULT '',  `User` char(32) COLLATE utf8_bin NOT NULL DEFAULT '',  `Table_name` char(64) COLLATE utf8_bin NOT NULL DEFAULT '',  `Column_name` char(64) COLLATE utf8_bin NOT NULL DEFAULT '',  `Timestamp` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,  `Column_priv` set('Select','Insert','Update','References') CHARACTER SET utf8 NOT NULL DEFAULT '',  PRIMARY KEY (`Host`,`Db`,`User`,`Table_name`,`Column_name`)) ENGINE=MyISAM DEFAULT CHARSET=utf8 COLLATE=utf8_bin COMMENT='Column privileges'",
			Fingerprint:      "",
			MinimumGrants:    nil,
			LastTestedGrants: nil,
			NotAllowed:       false,
			Error:            nil,
			InvalidQuery:     false,
		},
	}
	file := filepath.Join(tu.BaseDir(), "testdata/genlog")
	res, err := ReadGeneralLog(file)
	if err != nil {
		t.Errorf("Cannot parse general log file %s: %s", file, err)
	}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("Parsed queries don't match")
	}
}
