package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

type CloudflareConfig struct {
	Token, Domain string
}

type Config struct {
	ReturnIP, WebHook string
	Cloudflare        CloudflareConfig
}

type Zone struct {
	Result []struct {
		Name, ID string
	}
}

type DNSRecords struct {
	Result []struct {
		Name, ID, Content string
	}
}

type RecordInfo struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
}

const CloudflareURL = "https://api.cloudflare.com/client/v4/"

var Domain = regexp.MustCompile(`^((?:\w*?\.)*)([\w-]+?\.\w{2,5})$`)

func main() {
	//WebhookとIPを返すURLを取得する。
	config := readToken()

	//IPアドレスを取得する
	my_ip, _ := getIP(config.ReturnIP)

	//ルートドメインを取り出す
	result := Domain.FindStringSubmatch(config.Cloudflare.Domain)
	root_domain := result[2]
	sub_domain := result[1][:len(result[1])]
	_ = sub_domain

	//ドメインの一致するゾーンのIDを取得
	zone_id, err := getZoneID(config.Cloudflare.Token, root_domain)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	//現在一致するIPアドレスとサブドメインの一致するレコードのIDを取得
	now_IP, record_id, err := getRecord(config.Cloudflare.Token, zone_id, config.Cloudflare.Domain)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	context := ""
	fmt.Println(now_IP, my_ip)
	if now_IP == my_ip {
		context = "DNS更新はありませんでした。"
		postMessage(config.WebHook, context)
		os.Exit(0)
	}

	data := RecordInfo{
		Type:    "A",
		Name:    config.Cloudflare.Domain,
		Content: my_ip,
		TTL:     3600,
	}
	err = updateRecord(config.Cloudflare.Token, zone_id, record_id, data)
	if err != nil {
		fmt.Println(err)
	} else {
		context = fmt.Sprintf(`DNSを更新しました。\n%s : %s`, config.Cloudflare.Domain, my_ip)
		postMessage(config.WebHook, context)
	}

}

func getIP(url string) (string, error) {
	//IPを返すURL
	resp, err := http.Get(url)
	if err != nil {
		fmt.Println(err)
		return "", err
	}
	defer resp.Body.Close()

	//レスポンスをstringに直す
	byteArray, err := io.ReadAll(resp.Body)
	my_ip := string(byteArray)
	return my_ip, err
}

func postMessage(webhookURL string, content string) error {
	//IPアドレスの入ったjsonを用意する
	body := []byte(fmt.Sprintf("{\"content\":\"%s\"}", content))
	//fmt.Println(string(body))
	//HTTPリクエストを作成、bodyをバイト列に直す
	req, err := http.NewRequest("POST", webhookURL, bytes.NewReader(body))
	if err != nil {
		fmt.Println(err)
		return err
	}
	//ヘッダの設定
	req.Header.Set("Content-Type", "application/json")
	//リクエストする
	client := new(http.Client)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return err
	}
	//ステータスを表示する
	if resp.StatusCode == 204 {
		fmt.Printf("%s\n", content)
	} else {
		fmt.Printf("%s\n%s\n", resp.Status, content)
	}

	return nil
}

func getZoneID(Token string, Domain string) (string, error) {
	req, err := http.NewRequest("GET", CloudflareURL+"zones", nil)
	if err != nil {
		fmt.Println(err)
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+Token)
	client := new(http.Client)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return "", err
	}

	var zone Zone
	byteArray, _ := ioutil.ReadAll(resp.Body)
	json.Unmarshal(byteArray, &zone)
	for i := 0; i < len(zone.Result); i++ {
		if zone.Result[i].Name == Domain {
			return zone.Result[i].ID, nil
		}
	}
	return "", fmt.Errorf("Domain not found in zone.")
}

func getRecord(Token string, Zoneid string, SubDomain string) (string, string, error) {
	URL := fmt.Sprintf("%szones/%s/dns_records", CloudflareURL, Zoneid)
	req, err := http.NewRequest("GET", URL, nil)
	if err != nil {
		fmt.Println(err)
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+Token)
	client := new(http.Client)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return "", "", err
	}
	byteArray, _ := ioutil.ReadAll(resp.Body)
	//fmt.Println(string(byteArray))
	var records DNSRecords
	json.Unmarshal(byteArray, &records)
	for i := 0; i < len(records.Result); i++ {
		if records.Result[i].Name == SubDomain {
			return records.Result[i].Content, records.Result[i].ID, nil
		}
	}
	return "", "", fmt.Errorf("SubDomain not found in zone.")
}

func updateRecord(Token string, ZoneID string, RecordID string, RecordInfo RecordInfo) error {
	URL := fmt.Sprintf("%szones/%s/dns_records/%s", CloudflareURL, ZoneID, RecordID)
	body, _ := json.Marshal(RecordInfo)
	//HTTPリクエストを作成、bodyをバイト列に直す
	req, err := http.NewRequest("PUT", URL, bytes.NewReader(body))
	if err != nil {
		fmt.Println(err)
		return err
	}
	//ヘッダの設定
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+Token)
	//リクエストする
	client := new(http.Client)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return err
	}
	byteArray, _ := ioutil.ReadAll(resp.Body)
	var result interface{}
	json.Unmarshal(byteArray, &result)
	return nil
}

func readToken() Config {
	//実行ファイルと同じフォルダへのパスを作る
	exe, err := os.Executable()
	exe = filepath.Dir(exe)

	var config Config

	//ファイル名と結合してパスを作って読み込む
	bytes, err := os.ReadFile(filepath.Join(exe, "config.json"))
	if err != nil {
		fmt.Println(err)
	}

	json.Unmarshal(bytes, &config)

	//不要なスペースや改行を削除
	return config
}
