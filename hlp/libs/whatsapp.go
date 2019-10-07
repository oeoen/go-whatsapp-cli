package libs

import (
	"encoding/gob"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	qrterm "github.com/Baozisoftware/qrcode-terminal-go"
	whatsapp "github.com/Rhymen/go-whatsapp"
	waproto "github.com/Rhymen/go-whatsapp/binary/proto"

	"github.com/dimaskiddo/go-whatsapp-cli/hlp"
)

var WAConn *whatsapp.Conn

type WAHandler struct {
	SessionConn   *whatsapp.Conn
	SessionJid    string
	SessionTag    string
	SessionFile   string
	SessionStart  uint64
	ReconnectTime int
	IsTest        bool
}

func (wah *WAHandler) HandleError(err error) {
	_, eMatch := err.(*whatsapp.ErrConnectionFailed)
	if eMatch {
		if WASessionExist(wah.SessionFile) && wah.SessionConn != nil {
			hlp.LogPrintln(hlp.LogLevelWarn, "connection closed unexpetedly, reconnecting after "+strconv.Itoa(wah.ReconnectTime)+" seconds")

			wah.SessionStart = uint64(time.Now().Unix())
			<-time.After(time.Duration(wah.ReconnectTime) * time.Second)

			err := wah.SessionConn.Restore()
			if err != nil {
				hlp.LogPrintln(hlp.LogLevelError, err.Error())
			}
		} else {
			hlp.LogPrintln(hlp.LogLevelError, "connection closed unexpetedly")
		}
	} else {
		if strings.Contains(strings.ToLower(err.Error()), "server closed connection") {
			return
		}

		hlp.LogPrintln(hlp.LogLevelError, err.Error())
	}
}

func (wah *WAHandler) HandleTextMessage(data whatsapp.TextMessage) {
	if wah.IsTest && data.Info.RemoteJid != wah.SessionJid {
		return
	}

	msgText := strings.SplitN(strings.TrimSpace(data.Text), " ", 2)
	if msgText[0] != wah.SessionTag || data.Info.Timestamp < wah.SessionStart {
		return
	}

	resText, err := hlp.CMDExec(hlp.CMDList, strings.Split(msgText[1], " "), 0)
	if err != nil {
		if len(resText) == 0 {
			resText = []string{"Ouch, Got some error here while processing your request 🙈"}
		} else {
			resText[0] = "Ouch, Got some error here while processing your request 🙈\n" + resText[0]
		}

		hlp.LogPrintln(hlp.LogLevelError, err.Error())
	}

	for i := 0; i < len(resText); i++ {
		err := WAMessageText(wah.SessionConn, data.Info.RemoteJid, resText[i], data.Info.Id, data.Text, 0)
		if err != nil {
			hlp.LogPrintln(hlp.LogLevelError, err.Error())
		}
	}
}

func WASyncVersion(conn *whatsapp.Conn) (string, error) {
	versionServer, err := whatsapp.CheckCurrentServerVersion()
	if err != nil {
		return "", err
	}

	conn.SetClientVersion(versionServer[0], versionServer[1], versionServer[2])
	versionClient := conn.GetClientVersion()

	return "whatsapp version " + strconv.Itoa(versionClient[0]) +
		"." + strconv.Itoa(versionClient[1]) + "." + strconv.Itoa(versionClient[2]), nil
}

func WASessionInit(timeout int) (*whatsapp.Conn, error) {
	conn, err := whatsapp.NewConn(time.Duration(timeout) * time.Second)
	if err != nil {
		return nil, err
	}
	conn.SetClientName("Go WhatsApp CLI", "Go WhatsApp")

	info, err := WASyncVersion(conn)
	if err != nil {
		return nil, err
	}
	hlp.LogPrintln(hlp.LogLevelInfo, info)

	return conn, nil
}

func WASessionPing(conn *whatsapp.Conn) error {
	ok, err := conn.AdminTest()
	if !ok {
		if err != nil {
			return err
		} else {
			return errors.New("something when wrong while trying to ping, please check phone connectivity")
		}
	}

	return nil
}

func WASessionLoad(file string) (whatsapp.Session, error) {
	session := whatsapp.Session{}

	buffer, err := os.Open(file)
	if err != nil {
		return session, err
	}
	defer buffer.Close()

	err = gob.NewDecoder(buffer).Decode(&session)
	if err != nil {
		return session, err
	}

	return session, nil
}

func WASessionSave(file string, session whatsapp.Session) error {
	buffer, err := os.Create(file)
	if err != nil {
		return err
	}
	defer buffer.Close()

	err = gob.NewEncoder(buffer).Encode(session)
	if err != nil {
		return err
	}

	return nil
}

func WASessionExist(file string) bool {
	_, err := os.Stat(file)
	if err != nil {
		return false
	}

	return true
}

func WASessionLogin(conn *whatsapp.Conn, file string) error {
	if conn != nil {
		defer func() {
			_, _ = conn.Disconnect()
		}()

		if WASessionExist(file) {
			return errors.New("session file already exist, please logout first")
		}

		qrstr := make(chan string)
		go func() {
			term := qrterm.New()
			term.Get(<-qrstr).Print()
		}()

		session, err := conn.Login(qrstr)
		if err != nil {
			return err
		}

		err = WASessionSave(file, session)
		if err != nil {
			return err
		}

		err = WASessionPing(conn)
		if err != nil {
			return err
		}
	} else {
		return errors.New("connection is not valid")
	}

	return nil
}

func WASessionRestore(conn *whatsapp.Conn, file string) error {
	if conn != nil {
		if !WASessionExist(file) {
			_, _ = conn.Disconnect()

			return errors.New("session file doesn't exist, please login first")
		}

		session, err := WASessionLoad(file)
		if err != nil {
			_ = os.Remove(file)
			_, _ = conn.Disconnect()

			return errors.New("session not valid, removing session file")
		}

		session, err = conn.RestoreWithSession(session)
		if err != nil {
			_, _ = conn.Disconnect()

			return err
		}

		err = WASessionSave(file, session)
		if err != nil {
			return err
		}

		err = WASessionPing(conn)
		if err != nil {
			return err
		}
	} else {
		return errors.New("connection is not valid")
	}

	return nil
}

func WASessionLogout(conn *whatsapp.Conn, file string) error {
	if conn != nil {
		defer func() {
			_, _ = conn.Disconnect()
		}()

		err := conn.Logout()
		if err != nil {
			return err
		}

		_ = os.Remove(file)
	} else {
		return errors.New("connection is not valid")
	}

	return nil
}

func WAMessageText(conn *whatsapp.Conn, msgJID string, msgText string, msgQuotedID string, msgQuoted string, msgDelay int) error {
	if conn != nil {
		content := whatsapp.TextMessage{}

		if len(msgQuotedID) != 0 {
			quoted := &waproto.Message{
				Conversation: &msgQuoted,
			}

			content = whatsapp.TextMessage{
				Info: whatsapp.MessageInfo{
					RemoteJid:       msgJID,
					QuotedMessageID: msgQuotedID,
					QuotedMessage:   *quoted,
				},
				Text: msgText,
			}
		} else {
			content = whatsapp.TextMessage{
				Info: whatsapp.MessageInfo{
					RemoteJid: msgJID,
				},
				Text: msgText,
			}
		}

		<-time.After(time.Duration(msgDelay) * time.Second)

		_, err := conn.Send(content)
		if err != nil {
			switch strings.ToLower(err.Error()) {
			case "sending message timed out":
				return nil
			default:
				return err
			}
		}
	} else {
		return errors.New("connection is not valid")
	}

	return nil
}