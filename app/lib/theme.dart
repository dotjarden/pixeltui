import 'package:flutter/cupertino.dart';

/// pixeltui dark palette (Apple-Music / Spotify-style near-black with a vibrant
/// accent).
const kBg = Color(0xFF0B0B0F);
const kCard = Color(0xFF18181F);
const kCard2 = Color(0xFF222230);
const kAccent = Color(0xFF7D56F4);
const kAccent2 = Color(0xFFF25D94);
const kText = Color(0xFFF2F2F7);
const kMuted = Color(0xFF9A9AA6);

const kAccentGradient = LinearGradient(
  begin: Alignment.topLeft,
  end: Alignment.bottomRight,
  colors: [kAccent, kAccent2],
);

// Chrome heights (content area is inset by these so nothing renders behind the
// translucent header / tab bar / mini-player).
const kHeaderHeight = 50.0;
const kTabBarHeight = 54.0;
const kMiniHeight = 64.0;
