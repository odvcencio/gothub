package entityutil

import (
	"github.com/odvcencio/got/pkg/entity"
	"github.com/odvcencio/got/pkg/object"
)

// KindName maps an extracted entity kind to the persisted object string.
func KindName(k entity.EntityKind) string {
	switch k {
	case entity.KindPreamble:
		return "preamble"
	case entity.KindImportBlock:
		return "import"
	case entity.KindDeclaration:
		return "declaration"
	case entity.KindInterstitial:
		return "interstitial"
	default:
		return "unknown"
	}
}

// ExtractAndWriteEntityList extracts entities for blob content and writes entity
// objects + entity list object to the store.
//
// Returns (hash, true, nil) when extraction succeeded and was persisted,
// ("", false, nil) for unsupported/unparseable files or zero entities,
// and ("", false, err) for storage/read failures.
func ExtractAndWriteEntityList(store *object.Store, path string, blobHash object.Hash) (object.Hash, bool, error) {
	blob, err := store.ReadBlob(blobHash)
	if err != nil {
		return "", false, err
	}
	el, err := entity.Extract(path, blob.Data)
	if err != nil || len(el.Entities) == 0 {
		return "", false, nil
	}
	hash, err := WriteEntityList(store, el)
	if err != nil {
		return "", false, err
	}
	return hash, true, nil
}

// WriteEntityList persists each entity in order and writes the enclosing list.
func WriteEntityList(store *object.Store, el *entity.EntityList) (object.Hash, error) {
	entityRefs := make([]object.Hash, 0, len(el.Entities))
	for _, ent := range el.Entities {
		entHash, err := store.WriteEntity(&object.EntityObj{
			Kind:     KindName(ent.Kind),
			Name:     ent.Name,
			DeclKind: ent.DeclKind,
			Receiver: ent.Receiver,
			Body:     ent.Body,
			BodyHash: object.Hash(ent.BodyHash),
		})
		if err != nil {
			return "", err
		}
		entityRefs = append(entityRefs, entHash)
	}
	return store.WriteEntityList(&object.EntityListObj{
		Language:   el.Language,
		Path:       el.Path,
		EntityRefs: entityRefs,
	})
}
