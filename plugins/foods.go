package plugins

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"strings"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

// Foods provides short, local food and drink suggestions. The lists live in
// data/foods so operators can add regional dishes without changing code.
// Every request produces one bounded IRC message.
type Foods struct {
	items     map[string][]string
	aliases   map[string]string
	maxLength int
}

var foodCategories = []string{
	"food", "beer", "coffee", "tea", "wine", "korean", "japanese", "sushi", "ramen",
	"chinese", "indian", "thai", "mexican", "italian", "mediterranean", "american",
	"pizza", "taco", "burrito", "burger", "pasta", "dessert", "snack", "vietnamese",
	"filipino", "french", "spanish", "turkish", "ethiopian", "brazilian", "caribbean",
	"indonesian", "persian", "middle-eastern", "german", "british", "irish", "scottish",
	"welsh", "portuguese", "greek", "polish", "ukrainian", "russian", "swedish", "norwegian",
	"danish", "finnish", "dutch", "belgian", "austrian", "swiss", "czech", "hungarian",
	"romanian", "georgian", "moroccan", "nigerian", "south-african", "peruvian", "argentinian",
	"chilean", "colombian", "venezuelan", "cuban", "canadian", "australian", "new-zealand",
	"malaysian", "pakistani", "bangladeshi", "sri-lankan", "nepalese", "drinks", "soda", "juice",
	"water", "smoothie", "milkshake", "lemonade", "mocktail", "energy-drink", "sports-drink",
	"hot-chocolate", "kombucha", "bubble-tea",
}

func (p *Foods) Name() string { return "foods" }

func (p *Foods) Commands() []string {
	commands := append([]string{"food", "foods"}, foodCategories[1:]...)
	return commands
}

func (p *Foods) Help() string {
	return "!food [category] [nickname] — suggest a food or drink; try !german, !british, !japanese, !mexican, !soda, !juice, or !mocktail"
}

func (p *Foods) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.maxLength = c.Int("max_length", 240)
	if p.maxLength < 80 || p.maxLength > 400 {
		p.maxLength = 240
	}
	p.items = make(map[string][]string, len(foodCategories))
	dataDir := c.String("data_dir", "data/foods")
	for _, category := range foodCategories {
		p.items[category] = readFoodList(filepath.Join(dataDir, category+".txt"))
	}
	p.addFallbacks()
	p.aliases = map[string]string{
		"food": "food", "foods": "food", "beer": "beer", "coffee": "coffee", "java": "coffee",
		"tea": "tea", "wine": "wine", "korean": "korean", "kr": "korean", "japanese": "japanese",
		"japan": "japanese", "jp": "japanese", "sushi": "sushi", "ramen": "ramen", "chinese": "chinese",
		"indian": "indian", "thai": "thai", "mexican": "mexican", "italian": "italian",
		"mediterranean": "mediterranean", "american": "american", "pizza": "pizza", "taco": "taco",
		"burger": "burger", "pasta": "pasta", "dessert": "dessert", "snack": "snack", "vietnamese": "vietnamese", "vn": "vietnamese",
		"filipino": "filipino", "philippines": "filipino", "ph": "filipino", "french": "french", "fr": "french",
		"spanish": "spanish", "spain": "spanish", "es": "spanish", "turkish": "turkish", "tr": "turkish",
		"ethiopian": "ethiopian", "ethiopia": "ethiopian", "brazilian": "brazilian", "br": "brazilian",
		"caribbean": "caribbean", "indonesian": "indonesian", "indonesia": "indonesian", "id": "indonesian",
		"persian": "persian", "iranian": "persian", "middle-eastern": "middle-eastern", "middleeastern": "middle-eastern",
		"german": "german", "de": "german", "british": "british", "uk": "british", "gb": "british", "england": "british",
		"irish": "irish", "ireland": "irish", "ie": "irish", "scottish": "scottish", "scotland": "scottish",
		"welsh": "welsh", "wales": "welsh", "portuguese": "portuguese", "portugal": "portuguese", "pt": "portuguese",
		"greek": "greek", "greece": "greek", "gr": "greek", "polish": "polish", "poland": "polish", "pl": "polish",
		"ukrainian": "ukrainian", "ukraine": "ukrainian", "ua": "ukrainian", "russian": "russian", "russia": "russian", "ru": "russian",
		"swedish": "swedish", "sweden": "swedish", "se": "swedish", "norwegian": "norwegian", "norway": "norwegian", "no": "norwegian",
		"danish": "danish", "denmark": "danish", "dk": "danish", "finnish": "finnish", "finland": "finnish", "fi": "finnish",
		"dutch": "dutch", "netherlands": "dutch", "nl": "dutch", "belgian": "belgian", "belgium": "belgian", "be": "belgian",
		"austrian": "austrian", "austria": "austrian", "at": "austrian", "swiss": "swiss", "switzerland": "swiss", "ch": "swiss",
		"czech": "czech", "czechia": "czech", "cz": "czech", "hungarian": "hungarian", "hungary": "hungarian", "hu": "hungarian",
		"romanian": "romanian", "romania": "romanian", "ro": "romanian", "georgian": "georgian", "georgia": "georgian",
		"moroccan": "moroccan", "morocco": "moroccan", "nigerian": "nigerian", "nigeria": "nigerian",
		"south-african": "south-african", "southafrican": "south-african", "peruvian": "peruvian", "peru": "peruvian",
		"argentinian": "argentinian", "argentina": "argentinian", "chilean": "chilean", "chile": "chilean",
		"colombian": "colombian", "colombia": "colombian", "venezuelan": "venezuelan", "venezuela": "venezuelan",
		"cuban": "cuban", "cuba": "cuban", "canadian": "canadian", "canada": "canadian", "australian": "australian",
		"australia": "australian", "new-zealand": "new-zealand", "newzealand": "new-zealand", "nz": "new-zealand",
		"malaysian": "malaysian", "malaysia": "malaysian", "pakistani": "pakistani", "pakistan": "pakistani",
		"bangladeshi": "bangladeshi", "bangladesh": "bangladeshi", "sri-lankan": "sri-lankan", "srilankan": "sri-lankan",
		"nepalese": "nepalese", "nepal": "nepalese", "drinks": "drinks", "drink": "drinks", "soda": "soda", "soft-drink": "soda",
		"soft-drinks": "soda", "pop": "soda", "juice": "juice", "water": "water", "smoothie": "smoothie", "milkshake": "milkshake",
		"lemonade": "lemonade", "mocktail": "mocktail", "mocktails": "mocktail", "energy-drink": "energy-drink", "energy": "energy-drink",
		"sports-drink": "sports-drink", "sportsdrink": "sports-drink", "hot-chocolate": "hot-chocolate", "cocoa": "hot-chocolate",
		"kombucha": "kombucha", "bubble-tea": "bubble-tea", "boba": "bubble-tea",
	}
	return nil
}

func (p *Foods) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok {
		return false
	}
	category, exists := p.aliases[strings.ToLower(cmd)]
	if !exists {
		return false
	}

	arg = strings.TrimSpace(arg)
	target := ""
	if category == "food" && arg != "" {
		parts := strings.Fields(arg)
		if selected, ok := p.aliases[strings.ToLower(parts[0])]; ok && selected != "food" {
			category = selected
			arg = strings.TrimSpace(strings.TrimPrefix(arg, parts[0]))
		}
	}
	if arg != "" {
		target = truncateRunes(cleanExternalText(arg), 80)
	}

	if category == "food" {
		category = p.randomCategory()
	}
	choices := p.items[category]
	if len(choices) == 0 {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "that food list is unavailable"))
		return true
	}
	item := choices[rand.Intn(len(choices))]
	label := strings.Title(category)
	result := fmt.Sprintf("%s pick: %s", label, item)
	if target != "" {
		result = fmt.Sprintf("%s pick for %s: %s", label, target, item)
	}
	b.Send(m.ReplyTarget(), truncateRunes(ircColor(ircCyan, result), p.maxLength))
	return true
}

func (p *Foods) randomCategory() string {
	available := make([]string, 0, len(p.items))
	for _, category := range foodCategories[1:] {
		if len(p.items[category]) > 0 {
			available = append(available, category)
		}
	}
	if len(available) == 0 {
		return "food"
	}
	return available[rand.Intn(len(available))]
}

func (p *Foods) addFallbacks() {
	fallbacks := map[string][]string{
		"food":           {"a warm bowl of ramen", "a crispy chicken sandwich", "fresh fruit", "a plate of nachos"},
		"beer":           {"a crisp pilsner", "a West Coast IPA", "a dry Irish stout", "a Belgian tripel"},
		"coffee":         {"an espresso", "a flat white", "a cold brew", "a cappuccino"},
		"tea":            {"earl grey", "matcha", "oolong tea", "masala chai"},
		"wine":           {"a dry Riesling", "a Pinot Noir", "a Malbec", "a sparkling rosé"},
		"korean":         {"bibimbap", "bulgogi", "tteokbokki", "kimchi jjigae"},
		"japanese":       {"okonomiyaki", "katsu curry", "yakitori", "onigiri"},
		"sushi":          {"salmon nigiri", "tuna maki", "unagi roll", "vegetable futomaki"},
		"ramen":          {"shoyu ramen", "miso ramen", "tonkotsu ramen", "tantanmen"},
		"chinese":        {"mapo tofu", "dan dan noodles", "char siu", "jiaozi"},
		"indian":         {"chana masala", "palak paneer", "biryani", "masala dosa"},
		"thai":           {"pad thai", "green curry", "tom yum", "khao soi"},
		"mexican":        {"tacos al pastor", "chiles rellenos", "pozole", "enchiladas"},
		"italian":        {"margherita pizza", "lasagna", "risotto", "gnocchi"},
		"mediterranean":  {"falafel", "moussaka", "shakshuka", "spanakopita"},
		"american":       {"mac and cheese", "barbecue ribs", "clam chowder", "apple pie"},
		"pizza":          {"margherita pizza", "mushroom pizza", "Detroit-style pizza", "white pizza"},
		"taco":           {"fish taco", "carnitas taco", "barbacoa taco", "black bean taco"},
		"burrito":        {"bean and cheese burrito", "carne asada burrito", "California burrito", "breakfast burrito"},
		"burger":         {"classic cheeseburger", "veggie burger", "smash burger", "mushroom Swiss burger"},
		"pasta":          {"spaghetti carbonara", "cacio e pepe", "pesto linguine", "puttanesca"},
		"dessert":        {"tiramisu", "mochi", "crème brûlée", "frozen yogurt"},
		"snack":          {"edamame", "popcorn", "trail mix", "rice crackers"},
		"vietnamese":     {"pho", "banh mi", "bun cha", "fresh spring rolls"},
		"filipino":       {"chicken adobo", "sinigang", "lumpia", "pancit"},
		"french":         {"croque monsieur", "coq au vin", "ratatouille", "crêpes"},
		"spanish":        {"paella", "tortilla española", "patatas bravas", "gazpacho"},
		"turkish":        {"doner kebab", "lahmacun", "manti", "mercimek çorbası"},
		"ethiopian":      {"injera", "doro wat", "tibs", "shiro wat"},
		"brazilian":      {"feijoada", "churrasco", "coxinha", "pão de queijo"},
		"caribbean":      {"jerk chicken", "curry goat", "rice and peas", "roti"},
		"indonesian":     {"nasi goreng", "rendang", "satay", "gado-gado"},
		"persian":        {"chelo kebab", "ghormeh sabzi", "fesenjan", "tahdig"},
		"middle-eastern": {"hummus", "falafel", "shawarma", "kibbeh"},
		"german":         {"bratwurst", "schnitzel", "sauerbraten", "pretzel"},
		"british":        {"fish and chips", "shepherd's pie", "bangers and mash", "Sunday roast"},
		"irish":          {"Irish stew", "boxty", "colcannon", "coddle"},
		"scottish":       {"haggis", "Cullen skink", "neeps and tatties", "cranachan"},
		"welsh":          {"Welsh rarebit", "cawl", "laverbread", "Welsh cakes"},
		"portuguese":     {"bacalhau", "caldo verde", "francesinha", "pastel de nata"},
		"greek":          {"moussaka", "souvlaki", "pastitsio", "spanakopita"},
		"polish":         {"pierogi", "bigos", "barszcz", "placki ziemniaczane"},
		"ukrainian":      {"borscht", "varenyky", "holubtsi", "deruny"},
		"russian":        {"pelmeni", "beef stroganoff", "blini", "olivier salad"},
		"swedish":        {"meatballs", "gravlax", "Jansson's temptation", "kanelbulle"},
		"norwegian":      {"fårikål", "lutefisk", "raspeball", "smoked salmon"},
		"danish":         {"smørrebrød", "stegt flæsk", "frikadeller", "æbleskiver"},
		"finnish":        {"karjalanpiirakka", "lohikeitto", "poronkäristys", "salmiakki"},
		"dutch":          {"stamppot", "bitterballen", "erwtensoep", "poffertjes"},
		"belgian":        {"moules-frites", "waterzooi", "stoofvlees", "Belgian waffles"},
		"austrian":       {"Wiener schnitzel", "goulash", "Tafelspitz", "Sachertorte"},
		"swiss":          {"fondue", "raclette", "rösti", "Zürcher geschnetzeltes"},
		"czech":          {"svíčková", "goulash", "knedlíky", "trdelník"},
		"hungarian":      {"goulash", "chicken paprikash", "lángos", "dobos torte"},
		"romanian":       {"sarmale", "mămăligă", "mici", "ciorbă"},
		"georgian":       {"khachapuri", "khinkali", "lobio", "badrijani nigvzit"},
		"moroccan":       {"tagine", "couscous", "pastilla", "harira"},
		"nigerian":       {"jollof rice", "suya", "egusi soup", "pounded yam"},
		"south-african":  {"bobotie", "biltong", "boerewors", "bunny chow"},
		"peruvian":       {"ceviche", "lomo saltado", "aji de gallina", "papa a la huancaína"},
		"argentinian":    {"asado", "empanadas", "milanesa", "choripán"},
		"chilean":        {"pastel de choclo", "empanada de pino", "cazuela", "completo"},
		"colombian":      {"bandeja paisa", "arepas", "ajiaco", "sancocho"},
		"venezuelan":     {"arepas", "pabellón criollo", "hallacas", "cachapas"},
		"cuban":          {"ropa vieja", "cuban sandwich", "moros y cristianos", "picadillo"},
		"canadian":       {"poutine", "tourtière", "Montreal bagel", "Nanaimo bar"},
		"australian":     {"meat pie", "chicken parmigiana", "lamington", "Vegemite toast"},
		"new-zealand":    {"hangi", "meat pie", "pavlova", "hokey pokey ice cream"},
		"malaysian":      {"nasi lemak", "laksa", "char kway teow", "roti canai"},
		"pakistani":      {"nihari", "biryani", "haleem", "seekh kebab"},
		"bangladeshi":    {"ilish bhapa", "kacchi biryani", "panta bhat", "shorshe ilish"},
		"sri-lankan":     {"kottu roti", "hoppers", "lamprais", "pol sambol"},
		"nepalese":       {"momos", "dal bhat", "thukpa", "sel roti"},
		"drinks":         {"sparkling water", "orange juice", "iced tea", "hot chocolate"},
		"soda":           {"cola", "root beer", "ginger ale", "lemon-lime soda"},
		"juice":          {"orange juice", "apple juice", "grape juice", "cranberry juice"},
		"water":          {"still water", "sparkling water", "mineral water", "coconut water"},
		"smoothie":       {"berry smoothie", "mango smoothie", "green smoothie", "banana smoothie"},
		"milkshake":      {"vanilla milkshake", "chocolate milkshake", "strawberry milkshake", "peanut butter milkshake"},
		"lemonade":       {"classic lemonade", "pink lemonade", "mint lemonade", "sparkling lemonade"},
		"mocktail":       {"virgin mojito", "Shirley Temple", "Arnold Palmer", "cucumber lime spritz"},
		"energy-drink":   {"caffeinated energy drink", "guarana drink", "yerba mate energy drink", "electrolyte energy drink"},
		"sports-drink":   {"electrolyte drink", "isotonic drink", "coconut electrolyte water", "recovery drink"},
		"hot-chocolate":  {"classic hot chocolate", "Mexican hot chocolate", "white hot chocolate", "peppermint cocoa"},
		"kombucha":       {"ginger kombucha", "berry kombucha", "green tea kombucha", "lemon kombucha"},
		"bubble-tea":     {"taro bubble tea", "milk tea with tapioca", "brown sugar boba", "fruit tea with popping pearls"},
	}
	for category, items := range fallbacks {
		if len(p.items[category]) == 0 {
			p.items[category] = items
		}
	}
}

func readFoodList(path string) []string {
	items := readQuotes(path)
	filtered := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || strings.HasPrefix(item, "#") {
			continue
		}
		filtered = append(filtered, truncateRunes(cleanExternalText(item), 100))
	}
	return filtered
}
