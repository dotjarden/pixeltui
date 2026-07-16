// TypeScript mirrors of the pixeltui server DTOs. Field names follow the
// server's JSON tags (snake_case). See tui/server/server.go + handlers for the
// authoritative shapes; this file is a 1:1 map for the desktop client.

export interface Capabilities {
	start_station: boolean;
	go_to_artist: boolean;
	go_to_album: boolean;
	radio: boolean;
	download: boolean;
	lyrics: boolean;
	share_url?: string;
}

/** Opaque stream id carries its source prefix: `yt:` | `su:` | `lo:`. */
export interface Track {
	id: string;
	track: string;
	artist: string;
	album?: string;
	duration: number; // seconds
	art?: string; // absolute URL (yt thumb) or server-relative `/api/art?id=…` (su/lo)
	source: 'youtube' | 'subsonic' | 'local' | string;
	capabilities: Capabilities;
}

/** Client→server write shape (mirrors trackDTO minus source/capabilities). */
export interface TrackPayload {
	id: string;
	track: string;
	artist: string;
	album: string;
	duration: number;
	art: string;
}

export interface Album {
	title: string;
	artist: string;
	year?: number;
	browse_id: string;
	art?: string;
}

export interface ArtistHit {
	name: string;
	art?: string;
	browse_id?: string;
}
export interface Entities {
	artists: ArtistHit[];
	albums: Album[];
}

export interface ArtistStats {
	listeners?: number;
	playcount?: number;
	tags?: string[];
	bio?: string;
}
export interface SimilarArtist {
	name: string;
	art?: string;
	listeners?: number;
	browse_id?: string;
}
export interface ArtistPage {
	name: string;
	art?: string;
	top_songs: Track[];
	albums: Album[];
	singles: Album[];
	description?: string;
	stats?: ArtistStats;
	similar_artists?: SimilarArtist[];
}

/** Nonessential artist metadata loaded after the playable page is visible. */
export interface ArtistPageExtras {
	stats?: ArtistStats;
	similar_artists?: SimilarArtist[];
}

export interface AlbumPage {
	title: string;
	artist: string;
	year?: number;
	art?: string;
	description?: string;
	explicit?: boolean;
	tracks: Track[];
}

export interface HistoryEntry extends Track {
	played_at: number; // unix seconds
}

export interface StatsEntry {
	name: string;
	artist?: string;
	plays: number;
	art?: string;
	id?: string;
	source?: string;
}
export interface Stats {
	days: number;
	plays: number;
	unique_tracks: number;
	unique_artists: number;
	seconds: number;
	top_artists: StatsEntry[];
	top_tracks: StatsEntry[];
}

export interface Device {
	id: string;
	name: string;
	created: string; // RFC3339
	last_seen: string; // RFC3339
	current: boolean;
}

export interface LyricsLine {
	t: number; // seconds
	text: string;
}
export interface Lyrics {
	synced: LyricsLine[];
	plain: string;
}

export interface Charts {
	tracks: Track[];
	country: string;
}
export interface Mix {
	title: string;
	tag: string;
	tracks: Track[];
}
export interface Station {
	tag: string;
	tracks: Track[];
}

/** Subsonic playlist list entries use Go struct field names (no JSON tags). */
export interface SubsonicPlaylist {
	ID: string;
	Name: string;
	SongCount: number;
}

export interface Sources {
	sources: string[];
	name: string;
	endpoints: string[];
	features: Record<string, string[]>;
}

/** Current, single-use server pairing invitation for host administration. */
export interface PairingInfo {
	url: string;
	code: string;
	link: string;
}

export interface TrackInfoLastFM {
	listeners?: number;
	playcount?: number;
	tags?: string[];
	wiki?: string;
	album?: string;
	duration?: number;
}
export interface TrackInfoYouTube {
	video_id?: string;
	title?: string;
	channel?: string;
	upload_date?: string;
	views?: number;
	description?: string;
	license?: string;
	duration?: number;
}
export interface TrackInfoHistory {
	plays: number;
	first_played: number; // unix seconds
	last_played: number; // unix seconds
}
export interface TrackInfo {
	id: string;
	source: string;
	title: string;
	artist: string;
	album?: string;
	duration?: number;
	art?: string;
	lastfm?: TrackInfoLastFM;
	youtube?: TrackInfoYouTube;
	history?: TrackInfoHistory;
	lyrics?: boolean;
}

export interface PartyMember {
	id: string;
	name: string;
}
export interface PartySnapshot {
	code: string;
	rev: number;
	track?: Track;
	playing: boolean;
	paused: boolean;
	position: number;
	queue: Track[];
	members: PartyMember[];
	snapshot_unix_ms: number;
}

export interface Health {
	ok?: boolean;
	name: string;
	version?: string;
}
export interface Ok {
	ok: boolean;
}
export interface LikeResult {
	ok: boolean;
	liked: boolean;
}
export interface IdentifyResult {
	candidate: Track;
	score: number;
	source: string;
}
