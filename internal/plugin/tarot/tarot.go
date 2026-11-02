package tarot

import (
	"bytes"
	"embed"
	b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"math/rand"
	"path"
	"strconv"
	"strings"

	"github.com/aimerneige/yukichan-bot/internal/pkg/common"
	"github.com/disintegration/imaging"
	log "github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

const DeckImgPath = "assets/deck"
const maxDrawCount = 8

//go:embed assets/deck/deck.json
var deckMetadata []byte

var cardNames = loadCardNames()

func loadCardNames() map[string]string {
	var decks map[string][]string
	if err := json.Unmarshal(deckMetadata, &decks); err != nil {
		panic(fmt.Errorf("tarot: invalid deck metadata: %w", err))
	}
	names := make(map[string]string)
	for _, card := range decks["bilibili"] {
		id, name, ok := strings.Cut(card, "_")
		if ok {
			names[id] = name
		}
	}
	return names
}

//go:embed assets/deck
var deckEmbedFS embed.FS

//go:embed assets/tarot.jpg
var tarotCommandImg []byte

func init() {
	engine := zero.New()
	common.DefaultSingle.Apply(engine)

	engine.OnFullMatchGroup([]string{"塔罗", "塔罗牌", "tarot"}).
		SetPriority(7).
		SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			helpText := message.Text("支持指令\n「运势预测」或「今日运势」（随机抽取一张牌）\n「塔罗占卜」（过去、现在、未来三张牌）\n「抽塔罗牌 5」（抽取 1～8 张，不重复；省略张数默认 1 张）")
			helpImage := message.Image("base64://" + b64.StdEncoding.EncodeToString(tarotCommandImg))
			ctx.SendChain(helpText, helpImage)
		})
	engine.OnFullMatchGroup([]string{"运势预测", "今日运势"}).
		SetPriority(7).
		SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			ctx.Send(drawCard(1))
		})
	engine.OnFullMatch("塔罗占卜").
		SetPriority(7).
		SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			ctx.Send(drawCards(3, true))
		})
	engine.OnPrefix("抽塔罗牌").
		SetPriority(7).
		SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			count, err := parseDrawCount(ctx.State["args"].(string))
			if err != nil {
				ctx.Send(err.Error())
				return
			}
			ctx.Send(drawCard(count))
		})

	engine.UseMidHandler(common.DefaultSpeedLimit)
}

func parseDrawCount(args string) (int, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return 1, nil
	}
	count, err := strconv.Atoi(args)
	if err != nil || count < 1 || count > maxDrawCount {
		return 0, fmt.Errorf("请输入 1～%d 之间的整数，例如「抽塔罗牌 3」。", maxDrawCount)
	}
	return count, nil
}

func drawCard(number int) message.Message {
	return drawCards(number, false)
}

func drawCards(number int, spread bool) message.Message {
	if number < 1 || number > maxDrawCount {
		return message.Message{message.Text("抽牌数量须为 1～8 张。")}
	}
	theme := "classic"
	if rand.Intn(3) == 0 {
		theme = "bilibili"
	}
	deckPath := path.Join(DeckImgPath, theme)
	cardImages, err := fs.ReadDir(deckEmbedFS, deckPath)
	if err != nil {
		log.Errorln("[tarot]", "Fail to read card images.", err)
		return []message.MessageSegment{message.Text("发生错误，无法读取塔罗图片")}
	}
	cardImagePaths := make([]string, 0, 78)
	for _, card := range cardImages {
		if !card.IsDir() && strings.EqualFold(path.Ext(card.Name()), ".jpg") {
			_imgPath := path.Join(deckPath, card.Name())
			cardImagePaths = append(cardImagePaths, _imgPath)
		}
	}
	if len(cardImagePaths) < number {
		log.Errorln("[tarot]", "Not enough card images", deckPath)
		return message.Message{message.Text("牌组素材不完整，暂时无法抽牌。")}
	}
	cardResult := randomDraw(cardImagePaths, number)
	log.Debugln("[tarot]", fmt.Sprintf("cards: %v", cardResult))
	imgs := make(message.Message, 0, number*3)
	for i, imgPath := range cardResult {
		imgData, err := fs.ReadFile(deckEmbedFS, imgPath)
		// 读取图片
		if err != nil {
			log.Errorln("[tarot]", "Fail to read card image", err)
			imgs = append(imgs, message.Text("读取第 "+strconv.Itoa(i+1)+" 张牌的图片失败，请稍后再试。\n"))
			continue
		}
		// 翻转图片，实现正逆位
		reversed := rand.Intn(2) == 0
		if reversed {
			flippedImageData, err := rotateImage(imgData)
			if err != nil {
				log.Errorln("[tarot]", "Fail to flip card image", err)
				imgs = append(imgs, message.Text("生成第 "+strconv.Itoa(i+1)+" 张牌的图片失败，请稍后再试。\n"))
				continue
			} else {
				imgData = flippedImageData
			}
		}
		id := strings.SplitN(path.Base(imgPath), "_", 2)[0]
		name := cardNames[id]
		if name == "" {
			name = strings.TrimSuffix(path.Base(imgPath), path.Ext(imgPath))
		}
		position := fmt.Sprintf("第 %d 张", i+1)
		if spread && number == 3 {
			position = []string{"过去", "现在", "未来"}[i]
		}
		orientation := "正位"
		if reversed {
			orientation = "逆位"
		}
		imgs = append(imgs,
			message.Text(fmt.Sprintf("%s：%s（%s）\n", position, name, orientation)),
			message.Image("base64://"+b64.StdEncoding.EncodeToString(imgData)),
			message.Text("\n牌义："+meaningText(id, reversed)+"\n"),
		)
	}
	return imgs
}

// randomDraw 随机不放回抽取
func randomDraw(s []string, k int) []string {
	if k <= 0 {
		return nil
	}
	s = append([]string(nil), s...)
	n := len(s)
	if k > n {
		k = n
	}

	result := make([]string, k)
	for i := 0; i < k; i++ {
		j := rand.Intn(n-i) + i
		result[i] = s[j]
		s[i], s[j] = s[j], s[i]
	}

	return result
}

func rotateImage(imageData []byte) ([]byte, error) {
	// Decode the []byte into an image.Image.
	img, _, err := image.Decode(bytes.NewReader(imageData))
	if err != nil {
		log.Errorln("[tarot]", "Fail to decode image", err)
		return nil, err
	}

	// rotate image
	rotated := imaging.Rotate180(img)

	// Encode the rotated image as a []byte.
	var buf bytes.Buffer
	if err := png.Encode(&buf, rotated); err != nil {
		log.Errorln("[tarot]", "Fail to encode flipped image", err)
		return nil, err
	}

	// Return the flipped []byte image.
	flippedData := buf.Bytes()
	return flippedData, nil
}
