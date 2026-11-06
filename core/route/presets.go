package route

func DefaultRules() []Rule {
	makeRule := func(id, name, category, target, group string, domains, processes []string) Rule {
		return Rule{ID: id, Name: name, Category: category, Enabled: true, Route: target, AutoGroup: group, Domains: domains, Processes: processes, ProcessPaths: []string{}, IPCIDRs: []string{}}
	}
	rules := []Rule{
		makeRule("game-dota2", "Dota 2", "games", "direct", "default", []string{"dota2.com", "dota2.ru"}, []string{"dota2.exe"}),
		makeRule("game-cs2", "Counter-Strike 2", "games", "direct", "default", []string{"counter-strike.net"}, []string{"cs2.exe"}),
		makeRule("game-pubg", "PUBG: Battlegrounds", "games", "direct", "default", []string{"pubg.com", "pubg.net"}, []string{"TslGame.exe", "ExecPubg.exe"}),
		makeRule("game-valorant", "VALORANT", "games", "direct", "default", []string{"playvalorant.com"}, []string{"VALORANT.exe", "VALORANT-Win64-Shipping.exe"}),
		makeRule("game-fortnite", "Fortnite", "games", "direct", "default", []string{"fortnite.com"}, []string{"FortniteClient-Win64-Shipping.exe"}),
		makeRule("game-lol", "League of Legends", "games", "direct", "default", []string{"leagueoflegends.com"}, []string{"League of Legends.exe", "LeagueClient.exe", "LeagueClientUx.exe"}),
		makeRule("launcher-steam", "Steam: клиент, магазин и загрузки", "launchers", "direct", "default", []string{"steampowered.com", "steamcommunity.com", "steamgames.com", "steamusercontent.com", "steamcontent.com", "steamstatic.com", "steamserver.net"}, []string{"steam.exe", "steamwebhelper.exe"}),
		makeRule("launcher-epic", "Epic Games Launcher", "launchers", "direct", "default", []string{"epicgames.com", "epicgames.dev", "unrealengine.com"}, []string{"EpicGamesLauncher.exe", "EpicWebHelper.exe"}),
		makeRule("launcher-riot", "Riot Client", "launchers", "direct", "default", []string{"riotgames.com", "riotcdn.net"}, []string{"RiotClientServices.exe", "RiotClientUx.exe"}),
		makeRule("launcher-battlenet", "Battle.net", "launchers", "direct", "default", []string{"battle.net", "blizzard.com", "blzstatic.com"}, []string{"Battle.net.exe"}),
		makeRule("video-youtube", "YouTube", "video", "auto", "youtube", []string{"youtube.com", "youtu.be", "googlevideo.com", "ytimg.com", "youtubei.googleapis.com"}, nil),
		makeRule("social-discord", "Discord: чат и звонки", "social", "auto", "discord", []string{"discord.com", "discord.gg", "discordapp.com", "discordapp.net", "discord.media"}, []string{"Discord.exe", "DiscordPTB.exe", "DiscordCanary.exe"}),
		makeRule("social-telegram", "Telegram", "social", "auto", "telegram", []string{"telegram.org", "t.me", "telegram.me", "telesco.pe"}, []string{"Telegram.exe"}),
		makeRule("ai-openai", "ChatGPT и OpenAI", "ai", "vpn", "ai", []string{"openai.com", "chatgpt.com", "oaistatic.com", "oaiusercontent.com"}, nil),
		makeRule("ai-claude", "Claude и Anthropic", "ai", "vpn", "ai", []string{"anthropic.com", "claude.ai"}, nil),
		makeRule("ai-gemini", "Gemini", "ai", "vpn", "ai", []string{"gemini.google.com", "generativelanguage.googleapis.com"}, nil),
		makeRule("video-twitch", "Twitch", "video", "auto", "default", []string{"twitch.tv", "ttvnw.net", "jtvnw.net", "twitchcdn.net"}, nil),
		makeRule("video-netflix", "Netflix", "video", "vpn", "default", []string{"netflix.com", "nflxvideo.net", "nflximg.net", "nflxext.com", "nflxso.net"}, nil),
		makeRule("social-meta", "Instagram и Facebook", "social", "vpn", "default", []string{"instagram.com", "cdninstagram.com", "facebook.com", "fbcdn.net", "messenger.com"}, nil),
		makeRule("social-x", "X / Twitter", "social", "vpn", "default", []string{"x.com", "twitter.com", "twimg.com", "t.co"}, nil),
		makeRule("social-reddit", "Reddit", "social", "auto", "default", []string{"reddit.com", "redd.it", "redditstatic.com", "redditmedia.com"}, nil),
		makeRule("work-github", "GitHub", "work", "auto", "default", []string{"github.com", "githubusercontent.com", "githubassets.com"}, nil),
		makeRule("work-notion", "Notion", "work", "auto", "default", []string{"notion.so", "notion.site", "notion.com"}, []string{"Notion.exe"}),
		makeRule("work-google", "Google: поиск, почта и документы", "work", "direct", "default", []string{"google.com", "gmail.com", "googleusercontent.com", "gstatic.com"}, nil),
		makeRule("work-zoom", "Zoom", "work", "direct", "default", []string{"zoom.us", "zoom.com", "zoomcdn.net"}, []string{"Zoom.exe"}),
		makeRule("work-teams", "Microsoft Teams", "work", "direct", "default", []string{"teams.microsoft.com", "teams.live.com", "teams.cloud.microsoft"}, []string{"ms-teams.exe", "Teams.exe"}),
		makeRule("music-spotify", "Spotify", "other", "vpn", "default", []string{"spotify.com", "scdn.co", "spotifycdn.com"}, []string{"Spotify.exe"}),
		makeRule("local-yandex", "Яндекс", "other", "direct", "default", []string{"yandex.ru", "ya.ru", "yandex.net", "yastatic.net", "kinopoisk.ru"}, nil),
		makeRule("local-vk", "VK и VK Видео", "social", "direct", "default", []string{"vk.com", "vk.ru", "vkvideo.ru", "vkuseraudio.net", "vkuserlive.net"}, nil),
		makeRule("local-services", "Госуслуги и российские магазины", "other", "direct", "default", []string{"gosuslugi.ru", "ozon.ru", "wildberries.ru", "wb.ru", "avito.ru"}, nil),
		makeRule("downloads-torrent", "qBittorrent и Transmission", "downloads", "direct", "default", nil, []string{"qbittorrent.exe", "transmission-qt.exe"}),
	}
	for i := range rules {
		if rules[i].ID == "downloads-torrent" {
			rules[i].Enabled = false
		}
		if rules[i].ID == "social-telegram" {
			rules[i].IPCIDRs = []string{"149.154.160.0/20", "91.108.4.0/22", "91.108.8.0/22", "91.108.12.0/22", "91.108.16.0/22", "91.108.56.0/22"}
		}
	}
	return rules
}
