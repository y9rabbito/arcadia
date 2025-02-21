/*
Copyright 2024 KubeAGI.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package evaluation

import (
	"context"
	"fmt"
	"io"

	"k8s.io/client-go/dynamic"

	"github.com/kubeagi/arcadia/api/base/v1alpha1"
	"github.com/kubeagi/arcadia/pkg/appruntime"
	pkgdocumentloaders "github.com/kubeagi/arcadia/pkg/documentloaders"
)

// RagasDataRow which adapts to the Ragas evaluation framework
type RagasDataRow struct {
	// Question by QAGeneration or manually input
	Question string `json:"question"`
	// GroundTruths by QAGeneration or manually input
	GroundTruths []string `json:"ground_truths"`
	// Contexts by similarity search to knowledgebase
	Contexts []string `json:"contexts"`
	// Answer by Application
	Answer string `json:"answer"`
}

// RagasDatasetGenerator generates datasets which adapts to the ragas framework
type RagasDatasetGenerator struct {
	cli dynamic.Interface

	app appruntime.Application

	options *genOptions
}

func NewRagasDatasetGenerator(ctx context.Context, cli dynamic.Interface, app *v1alpha1.Application, genOptions ...GenOptions) (*RagasDatasetGenerator, error) {
	// set generation options
	genOpts := defaultGenOptions()
	for _, o := range genOptions {
		o(genOpts)
	}

	// output header
	err := genOpts.output.Output(RagasDataRow{
		Question:     "question",
		GroundTruths: []string{"ground_truths"},
		Contexts:     []string{"contexts"},
		Answer:       "answer",
	})
	if err != nil {
		return nil, err
	}

	runapp, err := appruntime.NewAppOrGetFromCache(ctx, cli, app)
	if err != nil {
		return nil, err
	}
	return &RagasDatasetGenerator{cli: cli, app: *runapp, options: genOpts}, nil
}

type genOptions struct {
	// questionColumn in csv file which has the question
	questionColumn string
	// groundTruthsColumn in csv file which has the correct answer
	groundTruthsColumn string

	output Output
}

func defaultGenOptions() *genOptions {
	return &genOptions{
		questionColumn:     "q",
		groundTruthsColumn: "a",
		output:             &PrintOutput{},
	}
}

func WithQuestionColumn(questionColumn string) GenOptions {
	return func(genOpts *genOptions) {
		genOpts.questionColumn = questionColumn
	}
}

func WithGroundTruthsColumn(groundTruthsColumn string) GenOptions {
	return func(genOpts *genOptions) {
		genOpts.groundTruthsColumn = groundTruthsColumn
	}
}

func WithOutput(output Output) GenOptions {
	return func(genOpts *genOptions) {
		genOpts.output = output
	}
}

type GenOptions func(*genOptions)

// Generate a test dataset from a file(csv)
func (eval *RagasDatasetGenerator) Generate(ctx context.Context, csvData io.Reader, genOptions ...GenOptions) error {
	// set or update options
	for _, o := range genOptions {
		o(eval.options)
	}

	// load csv to langchain documents
	loader := pkgdocumentloaders.NewQACSV(csvData, "", pkgdocumentloaders.WithQuestionColumn(eval.options.questionColumn), pkgdocumentloaders.WithAnswerColumn(eval.options.groundTruthsColumn))
	langchainDocuments, err := loader.Load(ctx)
	if err != nil {
		return err
	}

	// convert langchain documents to ragas dataset
	for _, doc := range langchainDocuments {
		ragasRow := RagasDataRow{
			Question:     doc.PageContent,
			GroundTruths: []string{doc.Metadata[eval.options.groundTruthsColumn].(string)},
		}

		// chat with application
		out, err := eval.app.Run(ctx, eval.cli, nil, appruntime.Input{Question: ragasRow.Question, NeedStream: false, History: nil})
		if err != nil {
			return err
		}
		ragasRow.Answer = out.Answer

		// handle context
		contexts := make([]string, len(out.References))
		for refIndex, reference := range out.References {
			contexts[refIndex] = reference.String()
		}
		ragasRow.Contexts = contexts

		if err = eval.options.output.Output(ragasRow); err != nil {
			return fmt.Errorf("output: %v", err)
		}
	}

	return nil
}
