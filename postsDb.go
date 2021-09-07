package main

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/araddon/dateparse"
	"github.com/thoas/go-funk"
)

func (a *goBlog) checkPost(p *post) (err error) {
	if p == nil {
		return errors.New("no post")
	}
	now := time.Now()
	// Fix content
	p.Content = strings.TrimSuffix(strings.TrimPrefix(p.Content, "\n"), "\n")
	// Fix date strings
	if p.Published != "" {
		p.Published, err = toLocal(p.Published)
		if err != nil {
			return err
		}
	}
	if p.Updated != "" {
		p.Updated, err = toLocal(p.Updated)
		if err != nil {
			return err
		}
	}
	// Check status
	if p.Status == "" {
		p.Status = statusPublished
	}
	// Cleanup params
	for key, value := range p.Parameters {
		if value == nil {
			delete(p.Parameters, key)
			continue
		}
		allValues := []string{}
		for _, v := range value {
			if v != "" {
				allValues = append(allValues, v)
			}
		}
		if len(allValues) >= 1 {
			p.Parameters[key] = allValues
		} else {
			delete(p.Parameters, key)
		}
	}
	// Check blog
	if p.Blog == "" {
		p.Blog = a.cfg.DefaultBlog
	}
	if _, ok := a.cfg.Blogs[p.Blog]; !ok {
		return errors.New("blog doesn't exist")
	}
	// Check if section exists
	if _, ok := a.cfg.Blogs[p.Blog].Sections[p.Section]; p.Section != "" && !ok {
		return errors.New("section doesn't exist")
	}
	// Check path
	if p.Path != "/" {
		p.Path = strings.TrimSuffix(p.Path, "/")
	}
	if p.Path == "" {
		if p.Section == "" {
			p.Section = a.cfg.Blogs[p.Blog].DefaultSection
		}
		if p.Slug == "" {
			random := generateRandomString(5)
			p.Slug = fmt.Sprintf("%v-%02d-%02d-%v", now.Year(), int(now.Month()), now.Day(), random)
		}
		published := timeNoErr(dateparse.ParseLocal(p.Published))
		pathTmplString := defaultIfEmpty(
			a.cfg.Blogs[p.Blog].Sections[p.Section].PathTemplate,
			"{{printf \""+a.getRelativePath(p.Blog, "/%v/%02d/%02d/%v")+"\" .Section .Year .Month .Slug}}",
		)
		pathTmpl, err := template.New("location").Parse(pathTmplString)
		if err != nil {
			return errors.New("failed to parse location template")
		}
		var pathBuffer bytes.Buffer
		err = pathTmpl.Execute(&pathBuffer, map[string]interface{}{
			"BlogPath": a.getRelativePath(p.Blog, ""),
			"Year":     published.Year(),
			"Month":    int(published.Month()),
			"Day":      published.Day(),
			"Slug":     p.Slug,
			"Section":  p.Section,
		})
		if err != nil {
			return errors.New("failed to execute location template")
		}
		p.Path = pathBuffer.String()
	}
	if p.Path != "" && !strings.HasPrefix(p.Path, "/") {
		return errors.New("wrong path")
	}
	return nil
}

func (a *goBlog) createPost(p *post) error {
	return a.createOrReplacePost(p, &postCreationOptions{new: true})
}

func (a *goBlog) replacePost(p *post, oldPath string, oldStatus postStatus) error {
	return a.createOrReplacePost(p, &postCreationOptions{new: false, oldPath: oldPath, oldStatus: oldStatus})
}

type postCreationOptions struct {
	new       bool
	oldPath   string
	oldStatus postStatus
}

func (a *goBlog) createOrReplacePost(p *post, o *postCreationOptions) error {
	// Check post
	if err := a.checkPost(p); err != nil {
		return err
	}
	// Save to db
	if err := a.db.savePost(p, o); err != nil {
		return err
	}
	// Reload post from database
	p, err := a.getPost(p.Path)
	if err != nil {
		// Failed to reload post from database
		return err
	}
	// Trigger hooks
	if p.Status == statusPublished || p.Status == statusUnlisted {
		if o.new || (o.oldStatus != statusPublished && o.oldStatus != statusUnlisted) {
			defer a.postPostHooks(p)
		} else {
			defer a.postUpdateHooks(p)
		}
	}
	// Purge cache
	a.cache.purge()
	return nil
}

// Save check post to database
func (db *database) savePost(p *post, o *postCreationOptions) error {
	// Check
	if !o.new && o.oldPath == "" {
		return errors.New("old path required")
	}
	// Lock post creation
	db.pcm.Lock()
	defer db.pcm.Unlock()
	// Build SQL
	var sqlBuilder strings.Builder
	var sqlArgs = []interface{}{dbNoCache}
	// Start transaction
	sqlBuilder.WriteString("begin;")
	// Delete old post
	if !o.new {
		sqlBuilder.WriteString("delete from posts where path = ?;delete from post_parameters where path = ?;")
		sqlArgs = append(sqlArgs, o.oldPath, o.oldPath)
	}
	// Insert new post
	sqlBuilder.WriteString("insert into posts (path, content, published, updated, blog, section, status, priority) values (?, ?, ?, ?, ?, ?, ?, ?);")
	sqlArgs = append(sqlArgs, p.Path, p.Content, toUTCSafe(p.Published), toUTCSafe(p.Updated), p.Blog, p.Section, p.Status, p.Priority)
	// Insert post parameters
	for param, value := range p.Parameters {
		for _, value := range value {
			if value != "" {
				sqlBuilder.WriteString("insert into post_parameters (path, parameter, value) values (?, ?, ?);")
				sqlArgs = append(sqlArgs, p.Path, param, value)
			}
		}
	}
	// Commit transaction
	sqlBuilder.WriteString("commit;")
	// Execute
	if _, err := db.exec(sqlBuilder.String(), sqlArgs...); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: posts.path") {
			return errors.New("post already exists at given path")
		}
		return err
	}
	// Update FTS index
	db.rebuildFTSIndex()
	return nil
}

func (a *goBlog) deletePost(path string) error {
	p, err := a.deletePostFromDb(path)
	if err != nil || p == nil {
		return err
	}
	// Purge cache
	a.cache.purge()
	// Trigger hooks
	a.postDeleteHooks(p)
	return nil
}

func (a *goBlog) deletePostFromDb(path string) (*post, error) {
	if path == "" {
		return nil, nil
	}
	a.db.pcm.Lock()
	defer a.db.pcm.Unlock()
	p, err := a.getPost(path)
	if err != nil {
		return nil, err
	}
	_, err = a.db.exec(
		`begin;
		delete from posts where path = ?;
		delete from post_parameters where path = ?;
		insert or ignore into deleted (path) values (?);
		commit;`,
		dbNoCache, p.Path, p.Path, p.Path,
	)
	if err != nil {
		return nil, err
	}
	a.db.rebuildFTSIndex()
	return p, nil
}

func (db *database) replacePostParam(path, param string, values []string) error {
	// Lock post creation
	db.pcm.Lock()
	defer db.pcm.Unlock()
	// Build SQL
	var sqlBuilder strings.Builder
	var sqlArgs = []interface{}{dbNoCache}
	// Start transaction
	sqlBuilder.WriteString("begin;")
	// Delete old post
	sqlBuilder.WriteString("delete from post_parameters where path = ? and parameter = ?;")
	sqlArgs = append(sqlArgs, path, param)
	// Insert new post parameters
	for _, value := range values {
		if value != "" {
			sqlBuilder.WriteString("insert into post_parameters (path, parameter, value) values (?, ?, ?);")
			sqlArgs = append(sqlArgs, path, param, value)
		}
	}
	// Commit transaction
	sqlBuilder.WriteString("commit;")
	// Execute
	if _, err := db.exec(sqlBuilder.String(), sqlArgs...); err != nil {
		return err
	}
	// Update FTS index
	db.rebuildFTSIndex()
	return nil
}

type postsRequestConfig struct {
	search                                      string
	blog                                        string
	path                                        string
	limit                                       int
	offset                                      int
	sections                                    []string
	status                                      postStatus
	taxonomy                                    *configTaxonomy
	taxonomyValue                               string
	parameter                                   string
	parameterValue                              string
	publishedYear, publishedMonth, publishedDay int
	randomOrder                                 bool
	priorityOrder                               bool
	withoutParameters                           bool
	withOnlyParameters                          []string
	withoutRenderedTitle                        bool
}

func buildPostsQuery(c *postsRequestConfig, selection string) (query string, args []interface{}) {
	var queryBuilder strings.Builder
	// Selection
	queryBuilder.WriteString("select ")
	queryBuilder.WriteString(selection)
	queryBuilder.WriteString(" from ")
	// Table
	if c.search != "" {
		queryBuilder.WriteString("posts_fts(@search)")
		args = append(args, sql.Named("search", c.search))
	} else {
		queryBuilder.WriteString("posts")
	}
	// Filter
	queryBuilder.WriteString(" where 1")
	if c.path != "" {
		queryBuilder.WriteString(" and path = @path")
		args = append(args, sql.Named("path", c.path))
	}
	if c.status != "" && c.status != statusNil {
		queryBuilder.WriteString(" and status = @status")
		args = append(args, sql.Named("status", c.status))
	}
	if c.blog != "" {
		queryBuilder.WriteString(" and blog = @blog")
		args = append(args, sql.Named("blog", c.blog))
	}
	if c.parameter != "" {
		if c.parameterValue != "" {
			queryBuilder.WriteString(" and path in (select path from post_parameters where parameter = @param and value = @paramval)")
			args = append(args, sql.Named("param", c.parameter), sql.Named("paramval", c.parameterValue))
		} else {
			queryBuilder.WriteString(" and path in (select path from post_parameters where parameter = @param and length(coalesce(value, '')) > 0)")
			args = append(args, sql.Named("param", c.parameter))
		}
	}
	if c.taxonomy != nil && len(c.taxonomyValue) > 0 {
		queryBuilder.WriteString(" and path in (select path from post_parameters where parameter = @taxname and lowerx(value) = lowerx(@taxval))")
		args = append(args, sql.Named("taxname", c.taxonomy.Name), sql.Named("taxval", c.taxonomyValue))
	}
	if len(c.sections) > 0 {
		queryBuilder.WriteString(" and section in (")
		for i, section := range c.sections {
			if i > 0 {
				queryBuilder.WriteString(", ")
			}
			named := "section" + strconv.Itoa(i)
			queryBuilder.WriteByte('@')
			queryBuilder.WriteString(named)
			args = append(args, sql.Named(named, section))
		}
		queryBuilder.WriteByte(')')
	}
	if c.publishedYear != 0 {
		queryBuilder.WriteString(" and substr(tolocal(published), 1, 4) = @publishedyear")
		args = append(args, sql.Named("publishedyear", fmt.Sprintf("%0004d", c.publishedYear)))
	}
	if c.publishedMonth != 0 {
		queryBuilder.WriteString(" and substr(tolocal(published), 6, 2) = @publishedmonth")
		args = append(args, sql.Named("publishedmonth", fmt.Sprintf("%02d", c.publishedMonth)))
	}
	if c.publishedDay != 0 {
		queryBuilder.WriteString(" and substr(tolocal(published), 9, 2) = @publishedday")
		args = append(args, sql.Named("publishedday", fmt.Sprintf("%02d", c.publishedDay)))
	}
	// Order
	queryBuilder.WriteString(" order by ")
	if c.randomOrder {
		queryBuilder.WriteString("random()")
	} else if c.priorityOrder {
		queryBuilder.WriteString("priority desc, published desc")
	} else {
		queryBuilder.WriteString("published desc")
	}
	// Limit & Offset
	if c.limit != 0 || c.offset != 0 {
		queryBuilder.WriteString(" limit @limit offset @offset")
		args = append(args, sql.Named("limit", c.limit), sql.Named("offset", c.offset))
	}
	return queryBuilder.String(), args
}

func (d *database) loadPostParameters(posts []*post, parameters ...string) (err error) {
	if len(posts) == 0 {
		return nil
	}
	// Build query
	var sqlArgs []interface{}
	var queryBuilder strings.Builder
	queryBuilder.WriteString("select path, parameter, value from post_parameters where")
	// Paths
	queryBuilder.WriteString(" path in (")
	for i, p := range posts {
		if i > 0 {
			queryBuilder.WriteString(", ")
		}
		named := "path" + strconv.Itoa(i)
		queryBuilder.WriteByte('@')
		queryBuilder.WriteString(named)
		sqlArgs = append(sqlArgs, sql.Named(named, p.Path))
	}
	queryBuilder.WriteByte(')')
	// Parameters
	if len(parameters) > 0 {
		queryBuilder.WriteString(" and parameter in (")
		for i, p := range parameters {
			if i > 0 {
				queryBuilder.WriteString(", ")
			}
			named := "param" + strconv.Itoa(i)
			queryBuilder.WriteByte('@')
			queryBuilder.WriteString(named)
			sqlArgs = append(sqlArgs, sql.Named(named, p))
		}
		queryBuilder.WriteByte(')')
	}
	// Order
	queryBuilder.WriteString(" order by id")
	// Query
	rows, err := d.query(queryBuilder.String(), sqlArgs...)
	if err != nil {
		return err
	}
	// Result
	var path, name, value string
	params := map[string]map[string][]string{}
	for rows.Next() {
		if err = rows.Scan(&path, &name, &value); err != nil {
			return err
		}
		m, ok := params[path]
		if !ok {
			m = map[string][]string{}
		}
		m[name] = append(m[name], value)
		params[path] = m
	}
	// Add to posts
	for _, p := range posts {
		p.Parameters = params[p.Path]
	}
	return nil
}

func (a *goBlog) getPosts(config *postsRequestConfig) (posts []*post, err error) {
	// Query posts
	query, queryParams := buildPostsQuery(config, "path, coalesce(content, ''), coalesce(published, ''), coalesce(updated, ''), blog, coalesce(section, ''), status, priority")
	rows, err := a.db.query(query, queryParams...)
	if err != nil {
		return nil, err
	}
	// Prepare row scanning
	var path, content, published, updated, blog, section, status string
	var priority int
	for rows.Next() {
		if err = rows.Scan(&path, &content, &published, &updated, &blog, &section, &status, &priority); err != nil {
			return nil, err
		}
		// Create new post, fill and add to list
		p := &post{
			Path:      path,
			Content:   content,
			Published: toLocalSafe(published),
			Updated:   toLocalSafe(updated),
			Blog:      blog,
			Section:   section,
			Status:    postStatus(status),
			Priority:  priority,
		}
		posts = append(posts, p)
	}
	if !config.withoutParameters {
		err = a.db.loadPostParameters(posts, config.withOnlyParameters...)
		if err != nil {
			return nil, err
		}
	}
	// Render post title
	if !config.withoutRenderedTitle {
		for _, p := range posts {
			if t := p.Title(); t != "" {
				p.RenderedTitle = a.renderMdTitle(t)
			}
		}
	}
	return posts, nil
}

func (a *goBlog) getPost(path string) (*post, error) {
	posts, err := a.getPosts(&postsRequestConfig{path: path, limit: 1})
	if err != nil {
		return nil, err
	} else if len(posts) == 0 {
		return nil, errPostNotFound
	}
	return posts[0], nil
}

func (d *database) countPosts(config *postsRequestConfig) (count int, err error) {
	query, params := buildPostsQuery(config, "path")
	row, err := d.queryRow("select count(distinct path) from ("+query+")", params...)
	if err != nil {
		return
	}
	err = row.Scan(&count)
	return
}

func (d *database) getPostPaths(status postStatus) ([]string, error) {
	var postPaths []string
	rows, err := d.query("select path from posts where status = @status", sql.Named("status", status))
	if err != nil {
		return nil, err
	}
	var path string
	for rows.Next() {
		_ = rows.Scan(&path)
		if path != "" {
			postPaths = append(postPaths, path)
		}
	}
	return postPaths, nil
}

func (a *goBlog) getRandomPostPath(blog string) (path string, err error) {
	sections, ok := funk.Keys(a.cfg.Blogs[blog].Sections).([]string)
	if !ok {
		return "", errors.New("no sections")
	}
	query, params := buildPostsQuery(&postsRequestConfig{randomOrder: true, limit: 1, blog: blog, sections: sections}, "path")
	row, err := a.db.queryRow(query, params...)
	if err != nil {
		return
	}
	err = row.Scan(&path)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errPostNotFound
	} else if err != nil {
		return "", err
	}
	return path, nil
}

func (d *database) allTaxonomyValues(blog string, taxonomy string) ([]string, error) {
	var values []string
	rows, err := d.query("select distinct value from post_parameters where parameter = @tax and length(coalesce(value, '')) > 0 and path in (select path from posts where blog = @blog and status = @status) order by value", sql.Named("tax", taxonomy), sql.Named("blog", blog), sql.Named("status", statusPublished))
	if err != nil {
		return nil, err
	}
	var value string
	for rows.Next() {
		if err = rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

const mediaUseSql = `
with mediafiles (name) as (values %s)
select name, count(path) as count from (
    select distinct m.name, p.path
    from mediafiles m, post_parameters p
    where instr(p.value, m.name) > 0
    union
    select distinct m.name, p.path
    from mediafiles m, posts_fts p
    where p.content match '"' || m.name || '"'
)
group by name;
`

func (db *database) usesOfMediaFile(names ...string) (counts map[string]int, err error) {
	sqlArgs := []interface{}{dbNoCache}
	var nameValues strings.Builder
	for i, n := range names {
		if i > 0 {
			nameValues.WriteString(", ")
		}
		named := "name" + strconv.Itoa(i)
		nameValues.WriteString("(@")
		nameValues.WriteString(named)
		nameValues.WriteByte(')')
		sqlArgs = append(sqlArgs, sql.Named(named, n))
	}
	rows, err := db.query(fmt.Sprintf(mediaUseSql, nameValues.String()), sqlArgs...)
	if err != nil {
		return nil, err
	}
	counts = map[string]int{}
	var name string
	var count int
	for rows.Next() {
		err = rows.Scan(&name, &count)
		if err != nil {
			return nil, err
		}
		counts[name] = count
	}
	return counts, nil
}
