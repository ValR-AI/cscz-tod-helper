# Notices

## Valve content

The images in `internal/web/ui/img/` (map thumbnails, Counter-Terrorist and Terrorist model thumbnails, tier badges,
lock icon and the Counter-Strike: Condition Zero logo) were converted from the files of Counter-Strike: Condition Zero.
They belong to Valve Corporation and are **not** covered by this project's MIT license.

The game rules built into `internal/game/data_gen.go` (reputation points, tier unlocks, enemy rosters, tasks,
teammate and terrorist statistics and weapon preferences) were read from the game's `CareerGame*.vdf` and
`BotCampaignProfile.db` files.

Counter-Strike, Counter-Strike: Condition Zero and Valve are trademarks of Valve Corporation.
This project is not affiliated with or endorsed by Valve.

## Other credits

- The first map of the `.czp` format came from the `CZProfile.bt` 010 Editor template by MuLLlaH9!.
- How the game reads bot profiles, adds bots by name, which weapons each side can buy, and how bots buy and react
  was checked against
  [ReGameDLL_CS](https://github.com/rehlds/ReGameDLL_CS), a reimplementation of the game code
  (`game_shared/bot/bot_profile.cpp`, `dlls/bot/states/cs_bot_buy.cpp`, `dlls/bot/cs_bot_manager.cpp`,
  `dlls/bot/cs_bot_vision.cpp`, `dlls/weapontype.cpp`, `dlls/client.cpp`).
- The LiveSplit One connection follows the server protocol of
  [livesplit-core](https://github.com/LiveSplit/livesplit-core) (`src/networking/server_protocol.rs`).

## Made with AI

This project was designed by ValR66 and written with the help of an AI assistant (Claude, by Anthropic).
