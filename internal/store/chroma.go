package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	chroma "github.com/amikos-tech/chroma-go/pkg/api/v2"
	"github.com/amikos-tech/chroma-go/pkg/embeddings"
)

// DefaultCollectionName is the single collection DocMCP indexes into. One
// collection keeps library and version filtering to metadata predicates.
const DefaultCollectionName = "docmcp_chunks"

// Metadata keys. These are part of the on-disk format: changing one invalidates
// existing indexes, so they are declared once here.
const (
	metaSourceID    = "source_id"
	metaLibraryID   = "library_id"
	metaVersion     = "version"
	metaDocumentID  = "document_id"
	metaURL         = "url"
	metaTitle       = "title"
	metaHeadingPath = "heading_path"
	metaChunkIndex  = "chunk_index"
	metaContentHash = "content_hash"
)

// identityMetaKey holds the provider/model/dimensions fingerprint of the vectors
// in this collection.
const identityMetaKey = "embedding_identity"

// listPageSize bounds one Get page so a large library cannot return everything
// in a single unbounded result.
const listPageSize = 1000

// ChromaStore persists chunks in a Chroma collection. Only Chroma's supported
// API is used: no reaching into its SQLite schema.
type ChromaStore struct {
	client     chroma.Client
	collection chroma.Collection

	mu     sync.Mutex
	closed bool
}

// ChromaConfig points the store at a local persistent Chroma.
type ChromaConfig struct {
	// Path is the data directory. Tests must pass a t.TempDir(): the store never
	// writes into a user's real index by accident.
	Path string

	// CollectionName defaults to DefaultCollectionName.
	CollectionName string
}

// collectionConfig declares the index's distance space explicitly.
//
// This is not cosmetic. Without a configuration, Chroma builds its own default
// MiniLM embedding function and dlopens the ONNX runtime — even though DocMCP
// always supplies pre-computed vectors. That made the store depend on a native
// library it never uses, and on a machine-mapped download that can crash the
// process outright (SIGBUS in pure-tokenizers' symbol lookup on CI).
//
// Declaring the space keeps the store free of any embedding function: it stores
// and searches vectors, and nothing more.
func collectionConfig() *chroma.CollectionConfigurationImpl {
	return chroma.NewCollectionConfigurationFromMap(map[string]any{
		"hnsw": map[string]any{
			// Cosine on normalized vectors: retrieval ranks identically to the
			// in-memory reference store the contract is checked against.
			"space":           "cosine",
			"ef_construction": 100,
			"ef_search":       10,
			"max_neighbors":   16,
			"num_threads":     2,
			"resize_factor":   10,
			"sync_threshold":  1000,
			"batch_size":      200,
		},
	})
}

// NewChromaStore opens a persistent Chroma at cfg.Path and ensures the
// collection exists.
func NewChromaStore(ctx context.Context, cfg ChromaConfig) (*ChromaStore, error) {
	if strings.TrimSpace(cfg.Path) == "" {
		return nil, fmt.Errorf("chroma store: path is required")
	}

	name := cfg.CollectionName
	if name == "" {
		name = DefaultCollectionName
	}

	// Embedded mode, not server mode: the server runtime binds a TCP port, and
	// two clients in one test run collide on it. Embedded keeps the runtime
	// in-process with no port at all.
	client, err := chroma.NewPersistentClient(
		chroma.WithPersistentPath(cfg.Path),
		chroma.WithPersistentAllowReset(true),
	)
	if err != nil {
		return nil, fmt.Errorf("chroma store: open %s: %w", cfg.Path, err)
	}

	collection, err := client.GetOrCreateCollection(ctx, name,
		chroma.WithConfigurationCreate(collectionConfig()))
	if err != nil {
		return nil, fmt.Errorf("chroma store: collection %q: %w", name, err)
	}

	return &ChromaStore{client: client, collection: collection}, nil
}

func (s *ChromaStore) UpsertChunks(ctx context.Context, chunks []Chunk, vectors [][]float32) error {
	if len(chunks) == 0 {
		return nil
	}

	ids := make([]chroma.DocumentID, len(chunks))
	texts := make([]string, len(chunks))
	metadatas := make([]chroma.DocumentMetadata, len(chunks))
	embedVecs := make([]embeddings.Embedding, len(chunks))

	for i, c := range chunks {
		if err := validateChunk(c); err != nil {
			return fmt.Errorf("chunks[%d]: %w", i, err)
		}

		vector := c.Embedding
		if i < len(vectors) && len(vectors[i]) > 0 {
			vector = vectors[i]
		}

		ids[i] = chroma.DocumentID(c.ID)
		texts[i] = c.Content
		metadatas[i] = chunkMetadata(c)
		embedVecs[i] = embeddings.NewEmbeddingFromFloat32(vector)
	}

	if err := s.collection.Upsert(ctx,
		chroma.WithIDs(ids...),
		chroma.WithTexts(texts...),
		chroma.WithMetadatas(metadatas...),
		chroma.WithEmbeddings(embedVecs...),
	); err != nil {
		return fmt.Errorf("chroma store: upsert %d chunks: %w", len(chunks), err)
	}

	return nil
}

func (s *ChromaStore) DeleteChunks(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	docIDs := make([]chroma.DocumentID, len(ids))
	for i, id := range ids {
		docIDs[i] = chroma.DocumentID(id)
	}

	if err := s.collection.Delete(ctx, chroma.WithIDs(docIDs...)); err != nil {
		return fmt.Errorf("chroma store: delete %d chunks: %w", len(ids), err)
	}
	return nil
}

func (s *ChromaStore) DeleteSourceChunks(ctx context.Context, sourceID string) error {
	err := s.collection.Delete(ctx,
		chroma.WithWhere(chroma.EqString(metaSourceID, sourceID)),
	)
	if err != nil {
		return fmt.Errorf("chroma store: delete chunks for source %q: %w", sourceID, err)
	}
	return nil
}

func (s *ChromaStore) GetChunk(ctx context.Context, id string) (Chunk, error) {
	result, err := s.collection.Get(ctx, chroma.WithIDs(chroma.DocumentID(id)))
	if err != nil {
		return Chunk{}, fmt.Errorf("chroma store: get chunk %q: %w", id, err)
	}

	chunks := chunksFromGet(result)
	if len(chunks) == 0 {
		return Chunk{}, fmt.Errorf("%w: %s", ErrChunkNotFound, id)
	}
	return chunks[0], nil
}

func (s *ChromaStore) ListChunks(ctx context.Context, filter ListFilter) ([]Chunk, error) {
	opts := []chroma.CollectionGetOption{
		chroma.WithInclude(chroma.IncludeDocuments, chroma.IncludeMetadatas),
	}
	if where := buildWhere(filter); where != nil {
		opts = append(opts, chroma.WithWhere(where))
	}

	var out []Chunk
	for offset := 0; ; offset += listPageSize {
		page, err := s.collection.Get(ctx,
			append(opts, chroma.WithLimit(listPageSize), chroma.WithOffset(offset))...)
		if err != nil {
			return nil, fmt.Errorf("chroma store: list chunks: %w", err)
		}

		pageChunks := chunksFromGet(page)
		out = append(out, pageChunks...)

		if len(pageChunks) < listPageSize {
			break
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *ChromaStore) CountChunks(ctx context.Context, libraryID string) (int, error) {
	// Chroma's Count takes no filter, so the count is derived from a scoped
	// listing. Libraries here are bounded by MaxPages, so this stays cheap.
	chunks, err := s.ListChunks(ctx, ListFilter{LibraryID: libraryID})
	if err != nil {
		return 0, fmt.Errorf("chroma store: count chunks: %w", err)
	}
	return len(chunks), nil
}

func (s *ChromaStore) Query(ctx context.Context, q Query) ([]Result, error) {
	if len(q.Embedding) == 0 {
		return nil, nil
	}
	if q.TopK <= 0 {
		q.TopK = 10
	}

	opts := []chroma.CollectionQueryOption{
		chroma.WithQueryEmbeddings(embeddings.NewEmbeddingFromFloat32(q.Embedding)),
		chroma.WithNResults(q.TopK),
		chroma.WithInclude(
			chroma.IncludeDocuments,
			chroma.IncludeMetadatas,
			chroma.IncludeDistances,
		),
	}

	filter := ListFilter{LibraryID: q.LibraryID, Version: q.Version}
	if where := buildWhere(filter); where != nil {
		opts = append(opts, chroma.WithWhere(where))
	}

	result, err := s.collection.Query(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("chroma store: query %q: %w", q.LibraryID, err)
	}

	return resultsFromQuery(result)
}

func (s *ChromaStore) SetIdentity(ctx context.Context, identity string) error {
	err := s.collection.ModifyMetadata(ctx, chroma.NewMetadataFromMap(
		map[string]any{identityMetaKey: identity},
	))
	if err != nil {
		return fmt.Errorf("chroma store: set identity: %w", err)
	}
	return nil
}

func (s *ChromaStore) Identity(_ context.Context) (string, error) {
	metadata := s.collection.Metadata()
	if metadata == nil {
		return "", nil
	}

	identity, ok := metadata.GetString(identityMetaKey)
	if !ok {
		return "", nil
	}
	return identity, nil
}

func (s *ChromaStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true
	return s.client.Close()
}

func chunkMetadata(c Chunk) chroma.DocumentMetadata {
	metadata := chroma.NewEmptyMetadata()
	metadata.SetString(metaSourceID, c.SourceID)
	metadata.SetString(metaLibraryID, c.LibraryID)
	metadata.SetString(metaVersion, c.Version)
	metadata.SetString(metaDocumentID, c.DocumentID)
	metadata.SetString(metaURL, c.URL)
	metadata.SetString(metaTitle, c.Title)
	metadata.SetString(metaHeadingPath, c.HeadingPath)
	metadata.SetString(metaContentHash, c.ContentHash)
	metadata.SetRaw(metaChunkIndex, int64(c.Index))
	return metadata
}

func buildWhere(filter ListFilter) chroma.WhereClause {
	var clauses []chroma.WhereClause

	if filter.SourceID != "" {
		clauses = append(clauses, chroma.EqString(metaSourceID, filter.SourceID))
	}
	if filter.LibraryID != "" {
		clauses = append(clauses, chroma.EqString(metaLibraryID, filter.LibraryID))
	}
	if filter.Version != "" {
		clauses = append(clauses, chroma.EqString(metaVersion, filter.Version))
	}

	switch len(clauses) {
	case 0:
		return nil
	case 1:
		return clauses[0]
	default:
		return chroma.And(clauses...)
	}
}

// chunksFromGet rebuilds chunks from a Get response, skipping rows with no ID so
// a partially-populated page cannot produce a zero-keyed chunk.
func chunksFromGet(result chroma.GetResult) []Chunk {
	ids := result.GetIDs()
	documents := result.GetDocuments()
	metadatas := result.GetMetadatas()

	out := make([]Chunk, 0, len(ids))
	for i, id := range ids {
		chunk := Chunk{ID: string(id)}
		if i < len(documents) {
			chunk.Content = documents[i].ContentString()
		}
		if i < len(metadatas) {
			applyMetadata(&chunk, metadatas[i])
		}
		out = append(out, chunk)
	}

	return out
}

// resultsFromQuery flattens Chroma's query groups into a single result list.
//
// It reads the ID/document/metadata/distance accessors rather than
// ToRecordsGroups: the record view is empty for some responses even when the
// groups hold results, and silently dropping every hit is worse than not using it.
func resultsFromQuery(result chroma.QueryResult) ([]Result, error) {
	idGroups := result.GetIDGroups()
	if len(idGroups) == 0 {
		return nil, nil
	}

	docGroups := result.GetDocumentsGroups()
	metaGroups := result.GetMetadatasGroups()
	distGroups := result.GetDistancesGroups()

	var out []Result
	for groupIndex, ids := range idGroups {
		var (
			documents []chroma.Document
			metadatas []chroma.DocumentMetadata
			distances embeddings.Distances
		)
		if groupIndex < len(docGroups) {
			documents = docGroups[groupIndex]
		}
		if groupIndex < len(metaGroups) {
			metadatas = metaGroups[groupIndex]
		}
		if groupIndex < len(distGroups) {
			distances = distGroups[groupIndex]
		}

		for i, id := range ids {
			chunk := Chunk{ID: string(id)}

			if i < len(documents) {
				chunk.Content = documents[i].ContentString()
			}
			if i < len(metadatas) {
				applyMetadata(&chunk, metadatas[i])
			}

			res := Result{Chunk: chunk}
			if i < len(distances) {
				res.Score = float64(distances[i])
			}
			out = append(out, res)
		}
	}

	return out, nil
}

func applyMetadata(chunk *Chunk, metadata chroma.DocumentMetadata) {
	if metadata == nil {
		return
	}

	if v, ok := metadata.GetString(metaSourceID); ok {
		chunk.SourceID = v
	}
	if v, ok := metadata.GetString(metaLibraryID); ok {
		chunk.LibraryID = v
	}
	if v, ok := metadata.GetString(metaVersion); ok {
		chunk.Version = v
	}
	if v, ok := metadata.GetString(metaDocumentID); ok {
		chunk.DocumentID = v
	}
	if v, ok := metadata.GetString(metaURL); ok {
		chunk.URL = v
	}
	if v, ok := metadata.GetString(metaTitle); ok {
		chunk.Title = v
	}
	if v, ok := metadata.GetString(metaHeadingPath); ok {
		chunk.HeadingPath = v
	}
	if v, ok := metadata.GetString(metaContentHash); ok {
		chunk.ContentHash = v
	}
	if v, ok := metadata.GetInt(metaChunkIndex); ok {
		chunk.Index = int(v)
	}
}
